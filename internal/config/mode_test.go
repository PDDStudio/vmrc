package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestUserPathsWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only path separators")
	}
	got := userPaths("windows", `C:\Users\dev\AppData\Local`)
	want := paths{
		ConfigPath: `C:\Users\dev\AppData\Local\vmrc\agent.json`,
		StateDir:   `C:\Users\dev\AppData\Local\vmrc\state`,
		BaseDir:    `C:\Users\dev\AppData\Local\vmrc\files`,
		BinDir:     `C:\Users\dev\AppData\Local\vmrc\bin`,
	}
	if got != want {
		t.Fatalf("userPaths windows:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestUserPathsUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only path separators")
	}
	got := userPaths("linux", "/home/dev/.local/state")
	want := paths{
		ConfigPath: "/home/dev/.local/state/vmrc/agent.json",
		StateDir:   "/home/dev/.local/state/vmrc/state",
		BaseDir:    "/home/dev/.local/state/vmrc/files",
		BinDir:     "/home/dev/.local/state/vmrc/bin",
	}
	if got != want {
		t.Fatalf("userPaths unix:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestDefaultsForUserModeIsAbsolute(t *testing.T) {
	cfg, err := DefaultsFor(ModeUser)
	if err != nil {
		t.Fatalf("DefaultsFor user: %v", err)
	}
	for _, p := range []string{cfg.StateDir, cfg.BaseDir, cfg.BinDir} {
		if p == "" || !filepath.IsAbs(p) {
			t.Fatalf("user-mode path not absolute: %q", p)
		}
	}
}

func TestDefaultsForSystemMatchesLegacy(t *testing.T) {
	cfg, err := DefaultsFor(ModeSystem)
	if err != nil {
		t.Fatalf("DefaultsFor system: %v", err)
	}
	legacy := Defaults()
	if cfg.StateDir != legacy.StateDir || cfg.BaseDir != legacy.BaseDir {
		t.Fatalf("system mode must match legacy defaults: %+v vs %+v", cfg, legacy)
	}
}
