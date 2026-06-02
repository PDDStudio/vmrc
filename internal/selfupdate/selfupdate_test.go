package selfupdate

import (
	"context"
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
}
