package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileEmptyPathIsNoop(t *testing.T) {
	in := Defaults()
	out, err := LoadFile(in, "")
	if err != nil {
		t.Fatalf("empty path should be no-op, got %v", err)
	}
	if out.ListenPort != in.ListenPort || out.BaseDir != in.BaseDir {
		t.Fatalf("empty path changed config: %+v", out)
	}
}

func TestLoadFileOverlays(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmrc.json")
	os.WriteFile(path, []byte(`{"base_dir":"/srv","read_globs":["*.log"],"listen_port":9000}`), 0o644)
	out, err := LoadFile(Defaults(), path)
	if err != nil {
		t.Fatalf("LoadFile err = %v", err)
	}
	if out.BaseDir != "/srv" || out.ListenPort != 9000 {
		t.Fatalf("overlay failed: %+v", out)
	}
	if len(out.ReadGlobs) != 1 || out.ReadGlobs[0] != "*.log" {
		t.Fatalf("ReadGlobs = %v", out.ReadGlobs)
	}
}

func TestLoadFileKeepsUnsetDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmrc.json")
	// Only base_dir set; listen_port must keep its default.
	os.WriteFile(path, []byte(`{"base_dir":"/srv"}`), 0o644)
	out, err := LoadFile(Defaults(), path)
	if err != nil {
		t.Fatalf("LoadFile err = %v", err)
	}
	if out.ListenPort != 47800 {
		t.Fatalf("unset field should keep default: ListenPort = %d", out.ListenPort)
	}
}

func TestLoadFileBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmrc.json")
	os.WriteFile(path, []byte(`{not json`), 0o644)
	if _, err := LoadFile(Defaults(), path); err == nil {
		t.Fatal("bad JSON must return an error")
	}
}

func TestLoadFileIgnoresSecretKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmrc.json")
	// token/authkey in the file must NOT populate the secret fields.
	os.WriteFile(path, []byte(`{"token":"leaked","authkey":"leaked"}`), 0o644)
	out, err := LoadFile(Defaults(), path)
	if err != nil {
		t.Fatalf("LoadFile err = %v", err)
	}
	if out.Token != "" || out.AuthKey != "" {
		t.Fatalf("secrets must not load from file: token=%q authkey=%q", out.Token, out.AuthKey)
	}
}
