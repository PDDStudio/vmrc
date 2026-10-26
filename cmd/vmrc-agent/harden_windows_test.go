//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
)

// Hermetic: only resolves paths for the current account, writes nothing.

func TestCheckProtectedBinDir(t *testing.T) {
	root, err := config.ProtectedUserRoot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.ToLower(root), strings.ToLower(config.SystemRoot())) {
		t.Fatalf("protected root %q must live under %q", root, config.SystemRoot())
	}
	ok := config.Config{BinDir: filepath.Join(root, "bin")}
	if err := checkProtectedBinDir(ok, config.ModeUser, config.LayoutProtected); err != nil {
		t.Fatalf("bin dir inside the protected root must pass: %v", err)
	}
	bad := config.Config{BinDir: `C:\Users\dev\AppData\Local\vmrc\bin`}
	if err := checkProtectedBinDir(bad, config.ModeUser, config.LayoutProtected); err == nil {
		t.Fatal("a user-writable bin_dir must be refused for the elevated task")
	}
	// Not applicable to --no-elevate (app-data) installs or system mode.
	if err := checkProtectedBinDir(bad, config.ModeUser, config.LayoutAppData); err != nil {
		t.Fatalf("app-data layout: %v", err)
	}
	if err := checkProtectedBinDir(bad, config.ModeSystem, config.LayoutAppData); err != nil {
		t.Fatalf("system mode: %v", err)
	}
}

func TestProtectedLayoutDefaults(t *testing.T) {
	root, err := config.ProtectedUserRoot()
	if err != nil {
		t.Fatal(err)
	}
	d, err := config.DefaultsForLayout(config.ModeUser, config.LayoutProtected)
	if err != nil {
		t.Fatal(err)
	}
	if !config.IsWithin(d.BinDir, root) {
		t.Errorf("protected BinDir %q not under %q", d.BinDir, root)
	}
	if config.IsWithin(d.StateDir, root) || config.IsWithin(d.BaseDir, root) {
		t.Errorf("state/base dirs stay per-user, got state=%q base=%q", d.StateDir, d.BaseDir)
	}
	p, err := config.ConfigPathForLayout(config.ModeUser, config.LayoutProtected)
	if err != nil || filepath.Dir(p) != root {
		t.Errorf("protected config path = %q (err %v), want under %q", p, err, root)
	}
}
