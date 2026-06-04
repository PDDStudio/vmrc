package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"strings"
	"testing"
)

func TestReadCapped(t *testing.T) {
	t.Run("under cap", func(t *testing.T) {
		b, err := readCapped(strings.NewReader("hello"), 10)
		if err != nil || string(b) != "hello" {
			t.Fatalf("readCapped under cap = %q, %v", b, err)
		}
	})
	t.Run("exactly at cap", func(t *testing.T) {
		b, err := readCapped(strings.NewReader("hello"), 5)
		if err != nil || string(b) != "hello" {
			t.Fatalf("readCapped at cap = %q, %v", b, err)
		}
	})
	t.Run("over cap fails, does not truncate", func(t *testing.T) {
		b, err := readCapped(strings.NewReader("hello world"), 5)
		if !errors.Is(err, errBinaryTooLarge) {
			t.Fatalf("readCapped over cap = %q, %v; want errBinaryTooLarge", b, err)
		}
	})
}

// archiveEntry is one file placed into a test archive.
type archiveEntry struct {
	name string
	body []byte
}

// makeTarGz builds an in-memory .tar.gz from entries.
func makeTarGz(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write(e.body); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// makeZip builds an in-memory .zip from entries.
func makeZip(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write(e.body); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestExtractBinary(t *testing.T) {
	bin := []byte("ELF...the vmrc-agent binary...")
	extras := []archiveEntry{
		{"README.md", []byte("readme")},
		{"LICENSE", []byte("mit")},
	}

	t.Run("tar.gz", func(t *testing.T) {
		arc := makeTarGz(t, append([]archiveEntry{{"vmrc-agent", bin}}, extras...)...)
		got, err := extractBinary(arc, "vmrc-agent_v0.1.0_linux_amd64.tar.gz", "vmrc-agent")
		if err != nil {
			t.Fatalf("extractBinary: %v", err)
		}
		if !bytes.Equal(got, bin) {
			t.Errorf("extracted %q, want %q", got, bin)
		}
	})

	t.Run("zip with .exe", func(t *testing.T) {
		arc := makeZip(t, append([]archiveEntry{{"vmrc-agent.exe", bin}}, extras...)...)
		got, err := extractBinary(arc, "vmrc-agent_v0.1.0_windows_amd64.zip", "vmrc-agent")
		if err != nil {
			t.Fatalf("extractBinary: %v", err)
		}
		if !bytes.Equal(got, bin) {
			t.Errorf("extracted %q, want %q", got, bin)
		}
	})

	t.Run("missing binary", func(t *testing.T) {
		arc := makeTarGz(t, extras...)
		if _, err := extractBinary(arc, "vmrc_v0.1.0_linux_amd64.tar.gz", "vmrc"); err == nil {
			t.Error("extractBinary(no binary) = nil error, want error")
		}
	})

	t.Run("zip-slip entry skipped", func(t *testing.T) {
		// A traversal entry must not be returned even if it base-matches.
		arc := makeZip(t, archiveEntry{"../vmrc", bin})
		if _, err := extractBinary(arc, "vmrc_v0.1.0_windows_amd64.zip", "vmrc"); err == nil {
			t.Error("extractBinary(zip-slip only) = nil error, want error")
		}
	})
}
