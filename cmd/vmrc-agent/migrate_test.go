package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyInstallDetectedByStateDir(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "tailscaled.state"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasLegacyState(state) {
		t.Fatalf("expected legacy state to be detected in %q", state)
	}
	if hasLegacyState(filepath.Join(root, "empty")) {
		t.Fatalf("empty/missing dir must not be detected as legacy")
	}
}

func TestMigrateCopiesStateAndConfig(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "state"), 0o700))
	must(os.WriteFile(filepath.Join(src, "state", "tailscaled.state"), []byte("NODE"), 0o600))
	must(os.WriteFile(filepath.Join(src, "agent.json"), []byte(`{"hostname":"vmrc-agent"}`), 0o644))

	if err := migrateData(src, dst); err != nil {
		t.Fatalf("migrateData: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "state", "tailscaled.state"))
	if err != nil || string(b) != "NODE" {
		t.Fatalf("state not preserved: %q err=%v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "agent.json")); err != nil {
		t.Fatalf("config not copied: %v", err)
	}
}

func TestShouldMigrate(t *testing.T) {
	userRoot := t.TempDir()
	legacyRoot := t.TempDir()
	os.MkdirAll(filepath.Join(legacyRoot, "state"), 0o700)
	os.WriteFile(filepath.Join(legacyRoot, "state", "tailscaled.state"), []byte("x"), 0o600)
	if !shouldMigrate(legacyRoot, userRoot) {
		t.Fatalf("expected migrate when legacy has state and user does not")
	}
	os.MkdirAll(filepath.Join(userRoot, "state"), 0o700)
	os.WriteFile(filepath.Join(userRoot, "state", "tailscaled.state"), []byte("x"), 0o600)
	if shouldMigrate(legacyRoot, userRoot) {
		t.Fatalf("expected no migrate when user state already present (idempotent)")
	}
}
