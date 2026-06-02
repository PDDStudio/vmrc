package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(path, []byte(`{"base_dir":"/srv","read_globs":["*.log"],"listen_port":9000}`), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(path, []byte(`{"base_dir":"/srv"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := LoadFile(Defaults(), path)
	if err != nil {
		t.Fatalf("LoadFile err = %v", err)
	}
	if out.ListenPort != 47800 {
		t.Fatalf("unset field should keep default: ListenPort = %d", out.ListenPort)
	}
}

func TestLoadFileOverlaysIdentityAllowlists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmrc.json")
	if err := os.WriteFile(path, []byte(`{"allowed_logins":["alice@example.com"],"allowed_tags":["tag:prod","tag:lab"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := LoadFile(Defaults(), path)
	if err != nil {
		t.Fatalf("LoadFile err = %v", err)
	}
	if len(out.AllowedLogins) != 1 || out.AllowedLogins[0] != "alice@example.com" {
		t.Fatalf("AllowedLogins = %v", out.AllowedLogins)
	}
	if len(out.AllowedTags) != 2 || out.AllowedTags[0] != "tag:prod" || out.AllowedTags[1] != "tag:lab" {
		t.Fatalf("AllowedTags = %v", out.AllowedTags)
	}
}

func TestLoadFileBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmrc.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(Defaults(), path); err == nil {
		t.Fatal("bad JSON must return an error")
	}
}

func TestLoadFileIgnoresSecretKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmrc.json")
	// token/authkey in the file must NOT populate the secret fields.
	if err := os.WriteFile(path, []byte(`{"token":"leaked","authkey":"leaked"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := LoadFile(Defaults(), path)
	if err != nil {
		t.Fatalf("LoadFile err = %v", err)
	}
	if out.Token != "" || out.AuthKey != "" {
		t.Fatalf("secrets must not load from file: token=%q authkey=%q", out.Token, out.AuthKey)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		wantErr  string // substring; "" means no error
		wantWarn bool   // expect at least one warning
	}{
		{
			name: "valid config",
			cfg:  Config{StateDir: "/var/lib/vmrc/state", BaseDir: "/var/lib/vmrc/files", ReadGlobs: []string{"*.log"}, ListenPort: 47800},
		},
		{
			name:    "relative base dir",
			cfg:     Config{StateDir: "/abs/state", BaseDir: "rel/files", ReadGlobs: []string{"*.log"}, ListenPort: 47800},
			wantErr: "base_dir must be an absolute path",
		},
		{
			name:    "relative state dir",
			cfg:     Config{StateDir: "rel/state", BaseDir: "/abs/files", ReadGlobs: []string{"*.log"}, ListenPort: 47800},
			wantErr: "state_dir must be an absolute path",
		},
		{
			name:    "port too low",
			cfg:     Config{StateDir: "/abs/state", BaseDir: "/abs/files", ReadGlobs: []string{"*.log"}, ListenPort: 0},
			wantErr: "listen_port",
		},
		{
			name:    "port too high",
			cfg:     Config{StateDir: "/abs/state", BaseDir: "/abs/files", ReadGlobs: []string{"*.log"}, ListenPort: 70000},
			wantErr: "listen_port",
		},
		{
			name:     "empty read globs warns but is valid",
			cfg:      Config{StateDir: "/abs/state", BaseDir: "/abs/files", ReadGlobs: nil, ListenPort: 47800},
			wantWarn: true,
		},
		{
			// Empty StateDir/BaseDir are intentionally skipped by RequireAbsPaths
			// (empty = "not set"); lock in that contract so a future change can't
			// silently start rejecting unset paths.
			name: "empty paths are skipped, not rejected",
			cfg:  Config{StateDir: "", BaseDir: "", ReadGlobs: []string{"*.log"}, ListenPort: 47800},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warns, err := Validate(tt.cfg)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
			if tt.wantWarn && len(warns) == 0 {
				t.Fatalf("expected at least one warning, got none")
			}
			if !tt.wantWarn && len(warns) > 0 {
				t.Fatalf("expected no warnings, got %v", warns)
			}
		})
	}
}
