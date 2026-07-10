package main

import (
	"errors"
	"os"
	"path/filepath"
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
