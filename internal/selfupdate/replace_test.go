package selfupdate

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTarget(t *testing.T, content string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "vmrc-agent")
	if err := os.WriteFile(target, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestApplyBinaryReplacesTarget(t *testing.T) {
	target := writeTarget(t, "OLD")
	if err := applyBinary([]byte("NEW"), target); err != nil {
		t.Fatalf("applyBinary: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "NEW" {
		t.Fatalf("target = %q, want NEW", b)
	}
	if _, err := os.Stat(stagingPath(target)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging file must not remain after a successful update: %v", err)
	}
}

func TestApplyBinaryRemovesPlantedStagingFile(t *testing.T) {
	target := writeTarget(t, "OLD")
	// A file pre-created at the staging path (e.g. by another local user) must
	// be discarded, not reused: minio opens it with O_TRUNC, not O_EXCL.
	if err := os.WriteFile(stagingPath(target), []byte("EVIL"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := applyBinary([]byte("NEW"), target); err != nil {
		t.Fatalf("applyBinary: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "NEW" {
		t.Fatalf("target = %q, want NEW", b)
	}
}

func TestApplyBinaryRefusesUnremovableStagingPath(t *testing.T) {
	target := writeTarget(t, "OLD")
	// A non-empty directory at the staging path cannot be removed with
	// os.Remove: the update must be refused and the target left untouched.
	staged := stagingPath(target)
	if err := os.MkdirAll(filepath.Join(staged, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applyBinary([]byte("NEW"), target); err == nil {
		t.Fatal("want an error when the staging path cannot be cleared")
	}
	if b, _ := os.ReadFile(target); string(b) != "OLD" {
		t.Fatalf("target must be untouched, got %q", b)
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := sha256.Sum256([]byte("DATA"))
	if err := verifyFileSHA256(p, good[:]); err != nil {
		t.Fatalf("matching file: %v", err)
	}
	bad := sha256.Sum256([]byte("OTHER"))
	if err := verifyFileSHA256(p, bad[:]); !errors.Is(err, ErrChecksum) {
		t.Fatalf("tampered file: want ErrChecksum, got %v", err)
	}
}

func TestStagingPathMatchesMinio(t *testing.T) {
	got := stagingPath(filepath.Join("dir", "vmrc-agent.exe"))
	if want := filepath.Join("dir", ".vmrc-agent.exe.new"); got != want {
		t.Fatalf("stagingPath = %q, want %q", got, want)
	}
}
