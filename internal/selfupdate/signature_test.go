package selfupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// genKey returns a fresh ed25519 keypair. Keys are generated per test run; no
// private key (test or otherwise) is ever committed to the repo.
func genKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignVerify(t *testing.T) {
	msg := []byte("abc123  vmrc_v0.2.0_linux_amd64.tar.gz\n")
	pubA, privA := genKey(t)
	pubB, privB := genKey(t)
	_, privC := genKey(t)

	sigA := SignChecksums(privA, msg)
	sigB := SignChecksums(privB, msg)
	sigC := SignChecksums(privC, msg)

	tampered := append([]byte(nil), msg...)
	tampered[0] = 'f'

	// Flip one bit inside the base64 payload of an otherwise valid signature.
	lines := strings.SplitN(string(sigA), "\n", 3)
	raw, _ := base64.StdEncoding.DecodeString(lines[1])
	raw[0] ^= 0x01
	badSig := []byte(lines[0] + "\n" + base64.StdEncoding.EncodeToString(raw) + "\n")

	tests := []struct {
		name    string
		msg     []byte
		sig     []byte
		keys    []ed25519.PublicKey
		wantErr bool
	}{
		{"round trip", msg, sigA, []ed25519.PublicKey{pubA}, false},
		{"tampered checksums", tampered, sigA, []ed25519.PublicKey{pubA}, true},
		{"wrong key", msg, sigC, []ed25519.PublicKey{pubA}, true},
		{"rotation: second key verifies", msg, sigB, []ed25519.PublicKey{pubA, pubB}, false},
		{"rotation: old key still verifies", msg, sigA, []ed25519.PublicKey{pubA, pubB}, false},
		{"corrupted signature bytes", msg, badSig, []ed25519.PublicKey{pubA}, true},
		{"no trusted keys", msg, sigA, nil, true},
		{"empty signature file", msg, nil, []ed25519.PublicKey{pubA}, true},
		{"garbage signature file", msg, []byte("not a signature\n"), []ed25519.PublicKey{pubA}, true},
		{"wrong header", msg, []byte(strings.Replace(string(sigA), signatureHeader, "other-v9", 1)), []ed25519.PublicKey{pubA}, true},
		{"truncated signature", msg, []byte(lines[0] + "\nAAAA\n"), []ed25519.PublicKey{pubA}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyChecksumsSignature(tc.msg, tc.sig, tc.keys)
			if tc.wantErr {
				if !errors.Is(err, ErrSignature) {
					t.Fatalf("VerifyChecksumsSignature = %v, want ErrSignature", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("VerifyChecksumsSignature = %v, want nil", err)
			}
		})
	}
}

func TestSignatureFormat(t *testing.T) {
	pub, priv := genKey(t)
	sig := SignChecksums(priv, []byte("x"))
	lines := strings.Split(strings.TrimRight(string(sig), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("signature has %d lines, want 2: %q", len(lines), sig)
	}
	if want := signatureHeader + " ed25519 " + KeyID(pub); lines[0] != want {
		t.Errorf("header = %q, want %q", lines[0], want)
	}
	raw, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil || len(raw) != ed25519.SignatureSize {
		t.Errorf("payload: len=%d err=%v, want %d raw bytes", len(raw), err, ed25519.SignatureSize)
	}
	// CRLF tolerance (a file round-tripped through a Windows editor).
	crlf := []byte(strings.ReplaceAll(string(sig), "\n", "\r\n"))
	if err := VerifyChecksumsSignature([]byte("x"), crlf, []ed25519.PublicKey{pub}); err != nil {
		t.Errorf("CRLF signature: %v", err)
	}
}

func TestPublicKeyFileRoundTrip(t *testing.T) {
	pub, _ := genKey(t)
	file := MarshalPublicKey(pub)
	if !strings.Contains(string(file), KeyID(pub)) {
		t.Errorf("public key file should name its key id: %q", file)
	}
	got, err := ParsePublicKey(file)
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	if !got.Equal(pub) {
		t.Error("parsed key differs from original")
	}

	bad := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"comments only", "# nothing here\n"},
		{"not base64", "%%%\n"},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("short")) + "\n"},
		{"two keys in one file", base64.StdEncoding.EncodeToString(pub) + "\n" + base64.StdEncoding.EncodeToString(pub) + "\n"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParsePublicKey([]byte(tc.in)); err == nil {
				t.Errorf("ParsePublicKey(%q) = nil error, want error", tc.in)
			}
		})
	}
}

func TestParsePrivateKey(t *testing.T) {
	pub, priv := genKey(t)
	seed := base64.StdEncoding.EncodeToString(priv.Seed())
	full := base64.StdEncoding.EncodeToString(priv)

	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"seed", seed, false},
		{"seed with whitespace", "  " + seed + "\n", false},
		{"full 64-byte key", full, false},
		{"empty", "", true},
		{"not base64", "!!!", true},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("short")), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePrivateKey(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !got.Public().(ed25519.PublicKey).Equal(pub) {
				t.Error("derived public key mismatch")
			}
		})
	}
}

func TestEmbeddedKeysParse(t *testing.T) {
	// Every committed keys/*.pub must parse; an unparsable embedded key is a
	// build defect, not something to discover at update time. Zero keys is a
	// valid (fail-closed) state until the maintainer runs keygen.
	keys, err := EmbeddedKeys()
	if err != nil {
		t.Fatalf("EmbeddedKeys: %v", err)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		id := KeyID(k)
		if seen[id] {
			t.Errorf("duplicate embedded key %s", id)
		}
		seen[id] = true
	}
}

func TestTrustedKeysFailClosed(t *testing.T) {
	pub, _ := genKey(t)
	// Injected keys win.
	got, err := Target{TrustedKeys: []ed25519.PublicKey{pub}}.trustedKeys()
	if err != nil || len(got) != 1 || !got[0].Equal(pub) {
		t.Fatalf("trustedKeys(injected) = %v, %v", got, err)
	}
	// With nothing injected the embedded set is used; if that is empty the
	// check must fail closed with a helpful ErrSignature.
	emb, _ := EmbeddedKeys()
	_, err = Target{}.trustedKeys()
	if len(emb) == 0 {
		if !errors.Is(err, ErrSignature) || !strings.Contains(err.Error(), "no release signing key") {
			t.Fatalf("trustedKeys(no embedded) = %v, want ErrSignature 'no release signing key'", err)
		}
		if !errors.Is(Target{}.SigningReady(), ErrSignature) {
			t.Fatal("SigningReady() should fail with no embedded key")
		}
	} else if err != nil {
		t.Fatalf("trustedKeys(embedded) = %v", err)
	}
}
