package config

import (
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
