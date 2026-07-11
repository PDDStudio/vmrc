package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// failAfterReader yields data up to failAfter bytes, then returns an error, to
// simulate a transfer that dies partway through.
type failAfterReader struct {
	data     []byte
	failAfter int
	read     int
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if r.read >= r.failAfter {
		return 0, errors.New("connection dropped mid-transfer")
	}
	n := copy(p, r.data[r.read:min(r.read+len(p), r.failAfter)])
	r.read += n
	return n, nil
}

func TestDownloadToWritesFileOnSuccess(t *testing.T) {
	local := filepath.Join(t.TempDir(), "out.bin")
	n, err := downloadTo(local, strings.NewReader("PACKED"))
	if err != nil || n != 6 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := os.ReadFile(local)
	if string(got) != "PACKED" {
		t.Fatalf("got %q", got)
	}
}

func TestDownloadToOverwritesExistingFile(t *testing.T) {
	// Runs on all platforms (no skip): os.Rename replaces an existing file on
	// Windows too (MoveFileEx with MOVEFILE_REPLACE_EXISTING), so a get onto an
	// existing local path must succeed and replace the content.
	dir := t.TempDir()
	local := filepath.Join(dir, "dest.bin")
	if err := os.WriteFile(local, []byte("OLD-CONTENT-LONGER"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := downloadTo(local, strings.NewReader("NEW"))
	if err != nil {
		t.Fatalf("overwrite must succeed, got %v", err)
	}
	got, _ := os.ReadFile(local)
	if string(got) != "NEW" || n != 3 {
		t.Fatalf("content = %q (n=%d), want NEW", got, n)
	}
}

func TestDownloadToNewFileMatchesOsCreatePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not meaningful on Windows")
	}
	dir := t.TempDir()
	// os.Create is the ground truth for new-file perms (0666 & ~umask); a fresh
	// download must match it rather than CreateTemp's 0600.
	ref := filepath.Join(dir, "ref")
	f, err := os.Create(ref)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	refFI, _ := os.Stat(ref)

	local := filepath.Join(dir, "dl")
	if _, err := downloadTo(local, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	dlFI, _ := os.Stat(local)
	if dlFI.Mode().Perm() != refFI.Mode().Perm() {
		t.Fatalf("new download perm %v != os.Create perm %v", dlFI.Mode().Perm(), refFI.Mode().Perm())
	}
}

func TestDownloadToPreservesExistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not meaningful on Windows")
	}
	dir := t.TempDir()
	local := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(local, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := downloadTo(local, strings.NewReader("new")); err != nil {
		t.Fatal(err)
	}
	// Replacing an existing file must keep its mode (e.g. an executable stays +x),
	// not drop to CreateTemp's 0600.
	fi, _ := os.Stat(local)
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("replacing must preserve mode, got %v want 0755", fi.Mode().Perm())
	}
}

func TestDownloadToPreservesExistingFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "important.bin")
	if err := os.WriteFile(local, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &failAfterReader{data: []byte("PARTIALPARTIAL"), failAfter: 5}
	if _, err := downloadTo(local, r); err == nil {
		t.Fatal("expected error from a failed transfer")
	}
	// A failed download must NOT clobber the pre-existing local file.
	got, _ := os.ReadFile(local)
	if string(got) != "ORIGINAL" {
		t.Fatalf("existing file was corrupted by a failed transfer: %q", got)
	}
	// No temp file may be left behind in the target directory.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("temp file left behind after failure: %v", names)
	}
}
