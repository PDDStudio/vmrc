package selfupdate

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Release signing.
//
// The release pipeline signs the exact bytes of checksums.txt with an ed25519
// private key held only in CI secrets and publishes the result as
// checksums.txt.sig. Every vmrc binary embeds the trusted public key(s) from
// keys/*.pub and refuses to apply an update unless that signature verifies
// BEFORE any checksum in checksums.txt is trusted. A valid signature over
// checksums.txt transitively authenticates every archive it lists.
//
// Signature file format (checksums.txt.sig), two LF- or CRLF-terminated lines:
//
//	vmrc-signature-v1 ed25519 <key-id>
//	<base64 (std, padded) of the raw 64-byte ed25519 signature>
//
// <key-id> is the first 8 bytes of SHA-256(public key) in lowercase hex. It is
// informational (diagnostics, rotation bookkeeping); verification tries every
// trusted key, so a mismatched id cannot cause a false accept.
//
// Public key file format (keys/*.pub): '#' comment lines and blank lines are
// ignored; exactly one remaining line holds the base64 of the raw 32-byte
// ed25519 public key. Several .pub files may be present at once to rotate keys.

// ErrSignature reports that a release's checksums.txt signature is missing,
// malformed, made by an untrusted key, or cannot be checked because this build
// has no trusted key embedded. Self-update always fails closed on it.
var ErrSignature = errors.New("release signature verification failed")

const (
	// SignatureAssetName is the release asset holding the detached signature
	// over checksums.txt.
	SignatureAssetName = "checksums.txt.sig"
	// SigningKeyEnv is the environment variable the release tooling reads the
	// base64 private key seed from. Never read by the vmrc binaries.
	SigningKeyEnv = "VMRC_RELEASE_SIGNING_KEY"

	signatureHeader  = "vmrc-signature-v1"
	signatureAlg     = "ed25519"
	maxSignatureSize = 4 << 10 // 4 KiB; a real one is ~120 bytes
)

//go:embed keys
var keysFS embed.FS

// KeyID returns the short identifier of pub: hex of the first 8 bytes of its
// SHA-256.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// MarshalPublicKey renders pub in the keys/*.pub file format.
func MarshalPublicKey(pub ed25519.PublicKey) []byte {
	return fmt.Appendf(nil,
		"# vmrc release signing public key (ed25519), key id %s.\n"+
			"# Embedded in vmrc binaries to verify %s. Safe to publish.\n%s\n",
		KeyID(pub), SignatureAssetName, base64.StdEncoding.EncodeToString(pub))
}

// ParsePublicKey parses a keys/*.pub file.
func ParsePublicKey(data []byte) (ed25519.PublicKey, error) {
	var key ed25519.PublicKey
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key != nil {
			return nil, fmt.Errorf("public key file holds more than one key (use one .pub file per key)")
		}
		raw, err := base64.StdEncoding.DecodeString(line)
		if err != nil {
			return nil, fmt.Errorf("public key is not valid base64: %w", err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("public key is %d bytes, want %d", len(raw), ed25519.PublicKeySize)
		}
		key = ed25519.PublicKey(raw)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if key == nil {
		return nil, fmt.Errorf("no public key found")
	}
	return key, nil
}

// ParsePrivateKey decodes a base64 ed25519 private key: either the 32-byte seed
// (what keygen prints) or the 64-byte expanded form. Surrounding whitespace is
// ignored.
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("private key is empty")
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("private key is not valid base64: %w", err)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		priv := ed25519.PrivateKey(raw)
		// Reject a 64-byte blob whose public half does not match its seed.
		if !ed25519.NewKeyFromSeed(raw[:ed25519.SeedSize]).Equal(priv) {
			return nil, fmt.Errorf("private key is inconsistent (public half does not match seed)")
		}
		return priv, nil
	default:
		return nil, fmt.Errorf("private key is %d bytes, want a %d-byte seed", len(raw), ed25519.SeedSize)
	}
}

// SignChecksums signs the exact checksums.txt bytes and returns the
// checksums.txt.sig file contents.
func SignChecksums(priv ed25519.PrivateKey, checksums []byte) []byte {
	sig := ed25519.Sign(priv, checksums)
	pub := priv.Public().(ed25519.PublicKey)
	return fmt.Appendf(nil, "%s %s %s\n%s\n",
		signatureHeader, signatureAlg, KeyID(pub), base64.StdEncoding.EncodeToString(sig))
}

// VerifyChecksumsSignature checks sigFile (checksums.txt.sig contents) against
// the exact checksums bytes, accepting a signature by any key in trusted. Every
// failure wraps ErrSignature.
func VerifyChecksumsSignature(checksums, sigFile []byte, trusted []ed25519.PublicKey) error {
	if len(trusted) == 0 {
		return fmt.Errorf("%w: no trusted release signing key", ErrSignature)
	}
	keyID, sig, err := parseSignature(sigFile)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrSignature, SignatureAssetName, err)
	}
	for _, k := range trusted {
		if len(k) == ed25519.PublicKeySize && ed25519.Verify(k, checksums, sig) {
			return nil
		}
	}
	return fmt.Errorf("%w: checksums.txt is not signed by a trusted key (signature names key id %s; trusted: %s)",
		ErrSignature, keyID, keyIDs(trusted))
}

func parseSignature(data []byte) (keyID string, sig []byte, err error) {
	var lines []string
	for l := range strings.SplitSeq(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 2 {
		return "", nil, fmt.Errorf("malformed signature file (want 2 lines, got %d)", len(lines))
	}
	hdr := strings.Fields(lines[0])
	if len(hdr) != 3 || hdr[0] != signatureHeader || hdr[1] != signatureAlg {
		return "", nil, fmt.Errorf("unsupported signature header %q (want %q)", lines[0], signatureHeader+" "+signatureAlg+" <key-id>")
	}
	sig, err = base64.StdEncoding.DecodeString(lines[1])
	if err != nil {
		return "", nil, fmt.Errorf("signature is not valid base64: %v", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return "", nil, fmt.Errorf("signature is %d bytes, want %d", len(sig), ed25519.SignatureSize)
	}
	return hdr[2], sig, nil
}

func keyIDs(keys []ed25519.PublicKey) string {
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, KeyID(k))
	}
	return strings.Join(ids, ", ")
}

// EmbeddedKeys returns the release signing public keys compiled into this
// binary (keys/*.pub). An empty result is valid and means self-update is
// disabled (fail closed); a malformed key file is an error.
func EmbeddedKeys() ([]ed25519.PublicKey, error) {
	entries, err := fs.ReadDir(keysFS, "keys")
	if err != nil {
		return nil, err
	}
	var keys []ed25519.PublicKey
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".pub" {
			continue
		}
		data, err := keysFS.ReadFile("keys/" + e.Name())
		if err != nil {
			return nil, err
		}
		k, err := ParsePublicKey(data)
		if err != nil {
			return nil, fmt.Errorf("embedded key %s: %w", e.Name(), err)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// trustedKeys returns the keys Apply verifies against: t.TrustedKeys when set
// (tests), else the embedded keys. It fails closed when there are none.
func (t Target) trustedKeys() ([]ed25519.PublicKey, error) {
	if len(t.TrustedKeys) > 0 {
		return t.TrustedKeys, nil
	}
	keys, err := EmbeddedKeys()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSignature, err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: this build has no release signing key embedded "+
			"(internal/selfupdate/keys/*.pub), so it cannot verify releases and self-update is disabled; "+
			"install a signed release build manually", ErrSignature)
	}
	return keys, nil
}

// SigningReady reports whether t can verify a release signature at all (i.e.
// has at least one trusted key). Callers that must do disruptive work before
// Apply — such as stopping a service — check it first so an unverifiable build
// fails before touching anything. Apply enforces the same condition itself.
func (t Target) SigningReady() error {
	_, err := t.trustedKeys()
	return err
}
