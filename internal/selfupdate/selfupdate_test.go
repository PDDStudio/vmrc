package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApply(t *testing.T) {
	t.Run("replaces the binary", func(t *testing.T) {
		f := &fakeGitHub{tag: "v0.2.0"}
		f.archive = makeTarGz(t, archiveEntry{"vmrc", []byte("BRAND-NEW-BINARY")})
		tgt := newFakeGitHub(t, f)

		rel, err := Check(context.Background(), tgt, Options{})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}

		exe := filepath.Join(t.TempDir(), "vmrc")
		if err := os.WriteFile(exe, []byte("OLD-BINARY"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := Apply(context.Background(), tgt, rel, exe); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		got, err := os.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "BRAND-NEW-BINARY" {
			t.Errorf("binary = %q, want replaced", got)
		}
		// No leftover temp files in the dir.
		entries, _ := os.ReadDir(filepath.Dir(exe))
		for _, e := range entries {
			if e.Name() != "vmrc" {
				t.Errorf("unexpected leftover file %q", e.Name())
			}
		}
	})

	t.Run("checksum mismatch leaves binary untouched", func(t *testing.T) {
		f := &fakeGitHub{tag: "v0.2.0"}
		f.archive = makeTarGz(t, archiveEntry{"vmrc", []byte("NEW")})
		tgt := newFakeGitHub(t, f)
		rel, err := Check(context.Background(), tgt, Options{})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		// Corrupt the served checksums AFTER Check resolved the asset.
		f.checksums = "deadbeef  " + f.assetName + "\n"

		exe := filepath.Join(t.TempDir(), "vmrc")
		if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
			t.Fatal(err)
		}
		err = Apply(context.Background(), tgt, rel, exe)
		if !errors.Is(err, ErrChecksum) {
			t.Fatalf("Apply = %v, want ErrChecksum", err)
		}
		got, _ := os.ReadFile(exe)
		if string(got) != "OLD" {
			t.Errorf("binary = %q, want unchanged (verify-before-swap)", got)
		}
	})

	// Signature failures: every case must leave the on-disk binary untouched and
	// must be reported as ErrSignature (fail closed).
	sigCases := []struct {
		name  string
		setup func(t *testing.T, f *fakeGitHub, tgt *Target, rel *Release)
	}{
		{"tampered checksums.txt", func(t *testing.T, f *fakeGitHub, _ *Target, _ *Release) {
			// Pin the signature over the ORIGINAL checksums, then swap in an
			// attacker's archive plus a checksums.txt that matches it.
			f.sig = SignChecksums(f.signKey, []byte(f.checksums))
			f.archive = makeTarGz(t, archiveEntry{"vmrc", []byte("EVIL")})
			sum := sha256.Sum256(f.archive)
			f.checksums = hex.EncodeToString(sum[:]) + "  " + f.assetName + "\n"
		}},
		{"signed by an untrusted key", func(t *testing.T, f *fakeGitHub, _ *Target, _ *Release) {
			_, other := genKey(t)
			f.sig = SignChecksums(other, []byte(f.checksums))
		}},
		{"garbage signature", func(_ *testing.T, f *fakeGitHub, _ *Target, _ *Release) {
			f.sig = []byte(signatureHeader + " ed25519 0000000000000000\nAAAA\n")
		}},
		{"signature asset stripped after Check", func(_ *testing.T, _ *fakeGitHub, _ *Target, rel *Release) {
			rel.SignatureURL = ""
		}},
		{"signature download 404", func(_ *testing.T, f *fakeGitHub, _ *Target, rel *Release) {
			rel.SignatureURL = f.srv.URL + "/missing/checksums.txt.sig"
		}},
		{"no trusted keys", func(t *testing.T, _ *fakeGitHub, tgt *Target, _ *Release) {
			if emb, _ := EmbeddedKeys(); len(emb) > 0 {
				t.Skip("this build embeds a release key; the no-key path is covered by TestTrustedKeysFailClosed")
			}
			tgt.TrustedKeys = nil
		}},
	}
	for _, tc := range sigCases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGitHub{tag: "v0.2.0"}
			f.archive = makeTarGz(t, archiveEntry{"vmrc", []byte("NEW")})
			tgt := newFakeGitHub(t, f)
			rel, err := Check(context.Background(), tgt, Options{})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			tc.setup(t, f, &tgt, &rel)

			exe := filepath.Join(t.TempDir(), "vmrc")
			if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
				t.Fatal(err)
			}
			err = Apply(context.Background(), tgt, rel, exe)
			if !errors.Is(err, ErrSignature) {
				t.Fatalf("Apply = %v, want ErrSignature", err)
			}
			if got, _ := os.ReadFile(exe); string(got) != "OLD" {
				t.Errorf("binary = %q, want unchanged", got)
			}
		})
	}

	t.Run("key rotation: release signed by second trusted key", func(t *testing.T) {
		oldPub, _ := genKey(t)
		_, newPriv := genKey(t)
		f := &fakeGitHub{tag: "v0.2.0", signKey: newPriv}
		f.archive = makeTarGz(t, archiveEntry{"vmrc", []byte("ROTATED")})
		tgt := newFakeGitHub(t, f)
		tgt.TrustedKeys = []ed25519.PublicKey{oldPub, newPriv.Public().(ed25519.PublicKey)}
		rel, err := Check(context.Background(), tgt, Options{})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		exe := filepath.Join(t.TempDir(), "vmrc")
		if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := Apply(context.Background(), tgt, rel, exe); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if got, _ := os.ReadFile(exe); string(got) != "ROTATED" {
			t.Errorf("binary = %q, want replaced", got)
		}
	})
}
