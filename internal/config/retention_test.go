package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionAndLogDefaults(t *testing.T) {
	sys, err := DefaultsFor(ModeSystem)
	if err != nil {
		t.Fatal(err)
	}
	user, err := DefaultsFor(ModeUser)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]Config{"Defaults": Defaults(), "system": sys, "user": user} {
		t.Run(name, func(t *testing.T) {
			if got := time.Duration(c.UploadRetention); got != 168*time.Hour {
				t.Errorf("UploadRetention = %v, want 168h", got)
			}
			if got := time.Duration(c.RetentionSweepInterval); got != time.Hour {
				t.Errorf("RetentionSweepInterval = %v, want 1h", got)
			}
			if c.LogMaxSizeMB != 10 {
				t.Errorf("LogMaxSizeMB = %d, want 10", c.LogMaxSizeMB)
			}
			if c.LogMaxBackups != 3 {
				t.Errorf("LogMaxBackups = %d, want 3", c.LogMaxBackups)
			}
			if c.UploadsDir != "" {
				t.Errorf("UploadsDir default must be empty (derived from base_dir), got %q", c.UploadsDir)
			}
		})
	}
}

func TestEffectiveUploadsDir(t *testing.T) {
	base := filepath.FromSlash("/srv/files")
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"derived from base_dir", Config{BaseDir: base}, filepath.Join(base, "uploads")},
		{"explicit wins", Config{BaseDir: base, UploadsDir: "/data/up"}, "/data/up"},
		{"no base, no uploads", Config{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.EffectiveUploadsDir(); got != tt.want {
				t.Fatalf("EffectiveUploadsDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadFileRetentionKeys(t *testing.T) {
	tests := []struct {
		name      string
		json      string
		wantRet   time.Duration
		wantSweep time.Duration
		wantSize  int
		wantBack  int
		wantUp    string
		wantErr   bool
	}{
		{name: "absent keys keep defaults", json: `{}`, wantRet: 168 * time.Hour, wantSweep: time.Hour, wantSize: 10, wantBack: 3},
		{name: "duration strings", json: `{"upload_retention":"24h","retention_sweep_interval":"15m"}`, wantRet: 24 * time.Hour, wantSweep: 15 * time.Minute, wantSize: 10, wantBack: 3},
		{name: "string zero disables", json: `{"upload_retention":"0"}`, wantRet: 0, wantSweep: time.Hour, wantSize: 10, wantBack: 3},
		{name: "numeric zero disables", json: `{"upload_retention":0}`, wantRet: 0, wantSweep: time.Hour, wantSize: 10, wantBack: 3},
		{name: "log rotation keys", json: `{"log_max_size_mb":0,"log_max_backups":7}`, wantRet: 168 * time.Hour, wantSweep: time.Hour, wantSize: 0, wantBack: 7},
		{name: "uploads dir", json: `{"uploads_dir":"/data/up"}`, wantRet: 168 * time.Hour, wantSweep: time.Hour, wantSize: 10, wantBack: 3, wantUp: "/data/up"},
		{name: "bad duration string", json: `{"upload_retention":"7 days"}`, wantErr: true},
		{name: "non-zero bare number is ambiguous", json: `{"upload_retention":3600}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.json")
			if err := os.WriteFile(path, []byte(tt.json), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := LoadFile(Defaults(), path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			if time.Duration(out.UploadRetention) != tt.wantRet || time.Duration(out.RetentionSweepInterval) != tt.wantSweep {
				t.Errorf("retention=%v sweep=%v, want %v/%v", time.Duration(out.UploadRetention), time.Duration(out.RetentionSweepInterval), tt.wantRet, tt.wantSweep)
			}
			if out.LogMaxSizeMB != tt.wantSize || out.LogMaxBackups != tt.wantBack {
				t.Errorf("log size=%d backups=%d, want %d/%d", out.LogMaxSizeMB, out.LogMaxBackups, tt.wantSize, tt.wantBack)
			}
			if out.UploadsDir != tt.wantUp {
				t.Errorf("UploadsDir = %q, want %q", out.UploadsDir, tt.wantUp)
			}
		})
	}
}

func TestDurationJSONRoundTrip(t *testing.T) {
	c := Defaults()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"upload_retention":"168h0m0s"`) {
		t.Errorf("upload_retention must marshal as a duration string; got %s", s)
	}
	if strings.Contains(s, `"uploads_dir"`) {
		t.Errorf("empty uploads_dir must be omitted so it keeps tracking base_dir; got %s", s)
	}
	var back Config
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.UploadRetention != c.UploadRetention || back.RetentionSweepInterval != c.RetentionSweepInterval {
		t.Errorf("round trip mismatch: %+v vs %+v", back, c)
	}
}

func TestValidateRetentionAndLogKeys(t *testing.T) {
	valid := func() Config {
		c := Defaults()
		c.StateDir, c.BaseDir, c.ReadGlobs = testAbs("/abs/state"), testAbs("/abs/files"), []string{"*.log"}
		return c
	}
	tests := []struct {
		name    string
		mut     func(*Config)
		wantErr string // substring; "" means valid
	}{
		{name: "defaults valid", mut: func(*Config) {}},
		{name: "retention disabled", mut: func(c *Config) { c.UploadRetention = 0 }},
		{name: "retention disabled ignores zero interval", mut: func(c *Config) { c.UploadRetention, c.RetentionSweepInterval = 0, 0 }},
		{name: "negative retention", mut: func(c *Config) { c.UploadRetention = Duration(-time.Hour) }, wantErr: "upload_retention"},
		{name: "negative sweep interval", mut: func(c *Config) { c.RetentionSweepInterval = Duration(-time.Minute) }, wantErr: "retention_sweep_interval"},
		{name: "zero sweep interval with retention on", mut: func(c *Config) { c.RetentionSweepInterval = 0 }, wantErr: "retention_sweep_interval"},
		{name: "negative log size", mut: func(c *Config) { c.LogMaxSizeMB = -1 }, wantErr: "log_max_size_mb"},
		{name: "negative log backups", mut: func(c *Config) { c.LogMaxBackups = -1 }, wantErr: "log_max_backups"},
		{name: "log rotation disabled", mut: func(c *Config) { c.LogMaxSizeMB = 0 }},
		{name: "relative uploads dir", mut: func(c *Config) { c.UploadsDir = "rel/up" }, wantErr: "uploads_dir must be an absolute path"},
		{name: "absolute uploads dir outside base", mut: func(c *Config) { c.UploadsDir = testAbs("/elsewhere/up") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			tt.mut(&c)
			_, err := Validate(c)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestEnsureDirsCreatesUploadsDir(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"derived", Config{BaseDir: filepath.Join(root, "a", "base")}, filepath.Join(root, "a", "base", "uploads")},
		{"explicit", Config{BaseDir: filepath.Join(root, "b", "base"), UploadsDir: filepath.Join(root, "b", "up")}, filepath.Join(root, "b", "up")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := EnsureDirs(tt.cfg); err != nil {
				t.Fatalf("EnsureDirs: %v", err)
			}
			if fi, err := os.Stat(tt.want); err != nil || !fi.IsDir() {
				t.Fatalf("uploads dir %q not created: %v", tt.want, err)
			}
		})
	}
}
