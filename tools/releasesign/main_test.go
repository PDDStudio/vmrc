package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pddstudio/vmrc/internal/selfupdate"
)

// testEnv builds an env with captured output, a fake environment and an
// injected trusted-key set (never the embedded one).
func testEnv(vars map[string]string, tty bool, trusted ...ed25519.PublicKey) (*env, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return &env{
		stdout:      &out,
		stderr:      &errb,
		getenv:      func(k string) string { return vars[k] },
		stdoutIsTTY: tty,
		trusted:     func() ([]ed25519.PublicKey, error) { return trusted, nil },
	}, &out, &errb
}

func TestKeygen(t *testing.T) {
	t.Run("writes public key, prints private seed to stdout only", func(t *testing.T) {
		dir := t.TempDir()
		pubPath := filepath.Join(dir, "keys", "release.pub")
		e, out, _ := testEnv(nil, false)
		if code := run(e, []string{"keygen", "-pub", pubPath}); code != 0 {
			t.Fatalf("keygen exit %d", code)
		}
		priv, err := selfupdate.ParsePrivateKey(out.String())
		if err != nil {
			t.Fatalf("stdout is not a private key: %v (%q)", err, out.String())
		}
		if strings.HasSuffix(out.String(), "\n") {
			t.Error("piped private key should have no trailing newline")
		}
		data, err := os.ReadFile(pubPath)
		if err != nil {
			t.Fatal(err)
		}
		pub, err := selfupdate.ParsePublicKey(data)
		if err != nil {
			t.Fatalf("written public key: %v", err)
		}
		if !pub.Equal(priv.Public()) {
			t.Fatal("public key file does not match printed private key")
		}
		// The private key must not appear in the public key file.
		if bytes.Contains(data, out.Bytes()) {
			t.Fatal("private key leaked into the public key file")
		}
		// Only the .pub file was written.
		entries, _ := os.ReadDir(filepath.Dir(pubPath))
		if len(entries) != 1 {
			t.Errorf("keygen wrote %d files, want 1", len(entries))
		}
	})

	tests := []struct {
		name     string
		args     []string
		tty      bool
		prewrite bool
	}{
		{"refuses a terminal stdout", []string{"keygen", "-pub", "PUB"}, true, false},
		{"refuses to overwrite an existing key", []string{"keygen", "-pub", "PUB"}, false, true},
		{"requires -pub", []string{"keygen"}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pubPath := filepath.Join(t.TempDir(), "release.pub")
			if tc.prewrite {
				if err := os.WriteFile(pubPath, []byte("existing"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			args := append([]string(nil), tc.args...)
			for i, a := range args {
				if a == "PUB" {
					args[i] = pubPath
				}
			}
			e, out, _ := testEnv(nil, tc.tty)
			if code := run(e, args); code == 0 {
				t.Fatal("exit 0, want failure")
			}
			if out.Len() != 0 {
				t.Errorf("stdout = %q, want nothing (no private key printed on failure)", out.String())
			}
			if !tc.prewrite {
				if _, err := os.Stat(pubPath); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("public key written despite failure (stat err %v)", err)
				}
			} else if b, _ := os.ReadFile(pubPath); string(b) != "existing" {
				t.Error("existing key file was modified")
			}
		})
	}

	t.Run("--force-tty prints to a terminal", func(t *testing.T) {
		pubPath := filepath.Join(t.TempDir(), "release.pub")
		e, out, _ := testEnv(nil, true)
		if code := run(e, []string{"keygen", "--force-tty", "-pub", pubPath}); code != 0 {
			t.Fatalf("exit %d", code)
		}
		if _, err := selfupdate.ParsePrivateKey(out.String()); err != nil {
			t.Fatalf("stdout: %v", err)
		}
	})
}

func TestSignAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed := base64.StdEncoding.EncodeToString(priv.Seed())
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)

	dir := t.TempDir()
	in := filepath.Join(dir, "checksums.txt")
	sums := []byte("0123abcd  vmrc_0.2.0_linux_amd64.tar.gz\n")
	if err := os.WriteFile(in, sums, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		vars    map[string]string
		trusted []ed25519.PublicKey
		ok      bool
		errLike string
	}{
		{"signs with a trusted key", map[string]string{selfupdate.SigningKeyEnv: seed}, []ed25519.PublicKey{pub}, true, ""},
		{"signs during rotation (key is second)", map[string]string{selfupdate.SigningKeyEnv: seed}, []ed25519.PublicKey{otherPub, pub}, true, ""},
		{"missing env var", nil, []ed25519.PublicKey{pub}, false, selfupdate.SigningKeyEnv},
		{"invalid env var", map[string]string{selfupdate.SigningKeyEnv: "not-a-key"}, []ed25519.PublicKey{pub}, false, selfupdate.SigningKeyEnv},
		{"key not embedded in binaries", map[string]string{selfupdate.SigningKeyEnv: seed}, []ed25519.PublicKey{otherPub}, false, "not one of the embedded"},
		{"no embedded keys at all", map[string]string{selfupdate.SigningKeyEnv: seed}, nil, false, "not one of the embedded"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "checksums.txt.sig")
			e, _, errb := testEnv(tc.vars, false, tc.trusted...)
			code := run(e, []string{"sign", "-in", in, "-out", out})
			if !tc.ok {
				if code == 0 {
					t.Fatal("exit 0, want failure")
				}
				if !strings.Contains(errb.String(), tc.errLike) {
					t.Errorf("stderr = %q, want mention of %q", errb.String(), tc.errLike)
				}
				if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
					t.Error("signature written despite failure")
				}
				return
			}
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errb.String())
			}
			sig, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if err := selfupdate.VerifyChecksumsSignature(sums, sig, []ed25519.PublicKey{pub}); err != nil {
				t.Fatalf("signature does not verify: %v", err)
			}
			if strings.Contains(string(sig), seed) {
				t.Fatal("private key leaked into signature file")
			}

			// The verify subcommand agrees.
			ve, _, verr := testEnv(nil, false, tc.trusted...)
			if code := run(ve, []string{"verify", "-in", in, "-sig", out}); code != 0 {
				t.Fatalf("verify exit %d: %s", code, verr.String())
			}
			// ...and rejects a tampered checksums.txt.
			bad := filepath.Join(t.TempDir(), "checksums.txt")
			_ = os.WriteFile(bad, append([]byte("f"), sums[1:]...), 0o644)
			if code := run(ve, []string{"verify", "-in", bad, "-sig", out}); code == 0 {
				t.Fatal("verify accepted tampered checksums.txt")
			}
		})
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}} {
		e, _, errb := testEnv(nil, false)
		if code := run(e, args); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if !strings.Contains(errb.String(), "usage") {
			t.Errorf("run(%v) stderr = %q, want usage", args, errb.String())
		}
	}
}
