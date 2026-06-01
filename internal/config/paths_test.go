package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	tests := []struct {
		goos                            string
		wantConfig, wantState, wantBase string
	}{
		{"windows", `C:\ProgramData\vmrc\agent.json`, `C:\ProgramData\vmrc\state`, `C:\ProgramData\vmrc\files`},
		{"linux", "/etc/vmrc/agent.json", "/var/lib/vmrc/state", "/var/lib/vmrc/files"},
		{"darwin", "/Library/Application Support/vmrc/agent.json", "/Library/Application Support/vmrc/state", "/Library/Application Support/vmrc/files"},
		{"freebsd", "/etc/vmrc/agent.json", "/var/lib/vmrc/state", "/var/lib/vmrc/files"}, // default branch
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			p := defaultPaths(tt.goos)
			if p.ConfigPath != tt.wantConfig {
				t.Errorf("ConfigPath = %q, want %q", p.ConfigPath, tt.wantConfig)
			}
			if p.StateDir != tt.wantState {
				t.Errorf("StateDir = %q, want %q", p.StateDir, tt.wantState)
			}
			if p.BaseDir != tt.wantBase {
				t.Errorf("BaseDir = %q, want %q", p.BaseDir, tt.wantBase)
			}
		})
	}
}

func TestDefaultsAreAbsolute(t *testing.T) {
	c := Defaults()
	if !filepath.IsAbs(c.StateDir) {
		t.Errorf("default StateDir not absolute: %q", c.StateDir)
	}
	if !filepath.IsAbs(c.BaseDir) {
		t.Errorf("default BaseDir not absolute: %q", c.BaseDir)
	}
	if c.ListenPort != 47800 {
		t.Errorf("ListenPort = %d, want 47800", c.ListenPort)
	}
}

func TestEnsureDirsCreatesMissing(t *testing.T) {
	root := t.TempDir()
	cfg := Config{
		StateDir: filepath.Join(root, "state"),
		BaseDir:  filepath.Join(root, "files"),
	}
	if err := EnsureDirs(cfg); err != nil {
		t.Fatalf("EnsureDirs err = %v", err)
	}
	st, err := os.Stat(cfg.StateDir)
	if err != nil {
		t.Fatalf("state dir not created: %v", err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Errorf("state dir perm = %o, want 700", st.Mode().Perm())
	}
	if _, err := os.Stat(cfg.BaseDir); err != nil {
		t.Fatalf("base dir not created: %v", err)
	}
}

func TestEnsureDirsIdempotent(t *testing.T) {
	root := t.TempDir()
	cfg := Config{StateDir: filepath.Join(root, "state")}
	if err := EnsureDirs(cfg); err != nil {
		t.Fatalf("first EnsureDirs err = %v", err)
	}
	if err := EnsureDirs(cfg); err != nil {
		t.Fatalf("second EnsureDirs err = %v", err)
	}
}

func TestRequireAbsPaths(t *testing.T) {
	if err := RequireAbsPaths(Config{StateDir: "/abs/state", BaseDir: "/abs/files"}); err != nil {
		t.Errorf("absolute paths should pass, got %v", err)
	}
	if err := RequireAbsPaths(Config{StateDir: "relative/state", BaseDir: "/abs"}); err == nil {
		t.Error("relative state_dir must be rejected")
	}
	if err := RequireAbsPaths(Config{StateDir: "/abs", BaseDir: "relative"}); err == nil {
		t.Error("relative base_dir must be rejected")
	}
}
