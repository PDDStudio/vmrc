package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRelocateBinaryCopies(t *testing.T) {
	src := filepath.Join(t.TempDir(), "vmrc-agent")
	if err := os.WriteFile(src, []byte("BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(t.TempDir(), "bin")
	dst, err := RelocateBinary(src, binDir, "vmrc-agent")
	if err != nil {
		t.Fatalf("RelocateBinary: %v", err)
	}
	if dst != filepath.Join(binDir, "vmrc-agent") {
		t.Fatalf("unexpected dst %q", dst)
	}
	if b, _ := os.ReadFile(dst); string(b) != "BINARY" {
		t.Fatalf("content mismatch: %q", b)
	}
}

func TestRelocateBinaryNoopWhenSamePath(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(binDir, "vmrc-agent")
	if err := os.WriteFile(src, []byte("X"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst, err := RelocateBinary(src, binDir, "vmrc-agent")
	if err != nil || dst != src {
		t.Fatalf("expected no-op same-path, got dst=%q err=%v", dst, err)
	}
}

func TestCopyTreeAndCopyFile(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "f"), []byte("DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "f")); string(b) != "DATA" {
		t.Fatalf("CopyTree content mismatch: %q", b)
	}
	single := filepath.Join(t.TempDir(), "deep", "one.json")
	if err := CopyFile(filepath.Join(src, "sub", "f"), single); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	if b, _ := os.ReadFile(single); string(b) != "DATA" {
		t.Fatalf("CopyFile content mismatch: %q", b)
	}
}
