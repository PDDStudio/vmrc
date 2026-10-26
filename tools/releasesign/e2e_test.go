package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pddstudio/vmrc/internal/selfupdate"
)

// TestEndToEnd exercises the whole release-signing chain with a throwaway key:
// the tool's keygen writes a public key and prints the private seed, the tool's
// sign signs a goreleaser-style checksums.txt with that seed from the env, and
// the selfupdate verifier (Check + Apply against a fake GitHub release) accepts
// it — and rejects the same release once checksums.txt is tampered with.
func TestEndToEnd(t *testing.T) {
	dir := t.TempDir()

	// 1. keygen (stdout piped, as in `... | gh secret set`).
	pubPath := filepath.Join(dir, "keys", "release.pub")
	ke, seedOut, kerr := testEnv(nil, false)
	if code := run(ke, []string{"keygen", "-pub", pubPath}); code != 0 {
		t.Fatalf("keygen: exit %d: %s", code, kerr.String())
	}
	pubFile, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := selfupdate.ParsePublicKey(pubFile)
	if err != nil {
		t.Fatal(err)
	}

	// 2. A release: one archive plus goreleaser's checksums.txt.
	const tag = "v0.2.0"
	asset := "vmrc_0.2.0_linux_amd64.tar.gz"
	archive := tarGz(t, "vmrc", []byte("E2E-NEW-BINARY"))
	sum := sha256.Sum256(archive)
	sums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)
	sumsPath := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(sumsPath, []byte(sums), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. sign, as goreleaser's signs step runs it (key from env; the key must be
	// one of the "embedded" keys, here the freshly generated one).
	sigPath := sumsPath + ".sig"
	se, _, serr := testEnv(map[string]string{selfupdate.SigningKeyEnv: seedOut.String()}, false, pub)
	if code := run(se, []string{"sign", "-in", sumsPath, "-out", sigPath}); code != 0 {
		t.Fatalf("sign: exit %d: %s", code, serr.String())
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		t.Fatal(err)
	}

	// 4. Fake GitHub serving archive + checksums.txt + checksums.txt.sig.
	served := sums
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": tag, "name": tag, "body": "notes",
				"assets": []map[string]string{
					{"name": asset, "browser_download_url": srv.URL + "/dl/" + asset},
					{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums.txt"},
					{"name": "checksums.txt.sig", "browser_download_url": srv.URL + "/dl/checksums.txt.sig"},
				},
			})
		case r.URL.Path == "/dl/checksums.txt.sig":
			_, _ = w.Write(sig)
		case r.URL.Path == "/dl/checksums.txt":
			_, _ = w.Write([]byte(served))
		case r.URL.Path == "/dl/"+asset:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	tgt := selfupdate.Target{
		Repo: "o/r", BinaryName: "vmrc", Version: "v0.1.0", GOOS: "linux", GOARCH: "amd64",
		HTTPClient: srv.Client(), APIBase: srv.URL, TrustedKeys: []ed25519.PublicKey{pub},
	}
	exe := filepath.Join(dir, "bin", "vmrc")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}

	// 5a. Tampered checksums.txt (attacker re-points the hash) is refused.
	served = strings.Repeat("0", 64) + "  " + asset + "\n"
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := selfupdate.Check(context.Background(), tgt, selfupdate.Options{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if err := selfupdate.Apply(context.Background(), tgt, rel, exe); !errors.Is(err, selfupdate.ErrSignature) {
		t.Fatalf("Apply(tampered) = %v, want ErrSignature", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD" {
		t.Fatalf("binary = %q after refused update, want unchanged", got)
	}

	// 5b. The genuine signed release verifies and is applied.
	served = sums
	if err := selfupdate.Apply(context.Background(), tgt, rel, exe); err != nil {
		t.Fatalf("Apply(signed): %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "E2E-NEW-BINARY" {
		t.Fatalf("binary = %q, want replaced", got)
	}
}

func tarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
