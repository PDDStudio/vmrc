package config

import (
	"os"
	"path/filepath"
	"runtime"
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
			if tt.goos != "windows" {
				// Some layouts are built with filepath.Join (host separator), so
				// compare in slash form.
				p.ConfigPath, p.StateDir, p.BaseDir = filepath.ToSlash(p.ConfigPath), filepath.ToSlash(p.StateDir), filepath.ToSlash(p.BaseDir)
			}
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

func TestLogPathFor(t *testing.T) {
	tests := []struct {
		name, goos, configPath, want string
	}{
		// Linux system mode logs under /var/log, not beside the config in /etc.
		{"linux system", "linux", "/etc/vmrc/agent.json", filepath.FromSlash("/var/log/vmrc/agent.log")},
		{"linux system custom name", "linux", "/etc/vmrc/other.json", filepath.FromSlash("/var/log/vmrc/agent.log")},
		{"linux user", "linux", "/home/u/.local/state/vmrc/agent.json", filepath.FromSlash("/home/u/.local/state/vmrc/logs/agent.log")},
		{"linux custom", "linux", "/opt/vmrc/agent.json", filepath.FromSlash("/opt/vmrc/logs/agent.log")},
		{"darwin system", "darwin", "/Library/Application Support/vmrc/agent.json", filepath.FromSlash("/Library/Application Support/vmrc/logs/agent.log")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := logPathFor(tt.goos, tt.configPath); got != tt.want {
				t.Errorf("logPathFor(%q, %q) = %q, want %q", tt.goos, tt.configPath, got, tt.want)
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
	if runtime.GOOS == "windows" {
		return // mode bits don't apply; Windows access is governed by DACLs
	}
	if st.Mode().Perm() != 0o700 {
		t.Errorf("state dir perm = %o, want 700", st.Mode().Perm())
	}
	bst, err := os.Stat(cfg.BaseDir)
	if err != nil {
		t.Fatalf("base dir not created: %v", err)
	}
	if bst.Mode().Perm() != 0o755 {
		t.Errorf("base dir perm = %o, want 755", bst.Mode().Perm())
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
	if err := RequireAbsPaths(Config{StateDir: testAbs("/abs/state"), BaseDir: testAbs("/abs/files")}); err != nil {
		t.Errorf("absolute paths should pass, got %v", err)
	}
	if err := RequireAbsPaths(Config{StateDir: "relative/state", BaseDir: testAbs("/abs")}); err == nil {
		t.Error("relative state_dir must be rejected")
	}
	if err := RequireAbsPaths(Config{StateDir: testAbs("/abs"), BaseDir: "relative"}); err == nil {
		t.Error("relative base_dir must be rejected")
	}
}
