package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/urfave/cli/v3"
)

// resolveWith runs the given command's flag set over args and resolves the
// config against a JSON file containing fileJSON.
func resolveWith(t *testing.T, flags []cli.Flag, args []string, fileJSON string) (config.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(fileJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	var rerr error
	cmd := &cli.Command{
		Flags: flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			cfg, rerr = resolveConfigFrom(cmd, config.Defaults(), path, true)
			return nil
		},
	}
	if err := cmd.Run(context.Background(), append([]string{"vmrc-agent"}, args...)); err != nil {
		return cfg, err
	}
	return cfg, rerr
}

func TestRetentionAndLogKeyPrecedence(t *testing.T) {
	abs := func(p string) string { return filepath.Join(string(filepath.Separator)+"abs", p) }
	baseJSON := `"state_dir":` + jsonStr(abs("state")) + `,"base_dir":` + jsonStr(abs("files")) + `,"read_globs":["*.log"]`
	tests := []struct {
		name  string
		file  string // extra JSON members
		env   map[string]string
		args  []string
		check func(t *testing.T, c config.Config)
	}{
		{
			name: "compiled defaults",
			check: func(t *testing.T, c config.Config) {
				if time.Duration(c.UploadRetention) != 168*time.Hour || time.Duration(c.RetentionSweepInterval) != time.Hour {
					t.Errorf("retention=%s sweep=%s", c.UploadRetention, c.RetentionSweepInterval)
				}
				if c.LogMaxSizeMB != 10 || c.LogMaxBackups != 3 {
					t.Errorf("log size=%d backups=%d", c.LogMaxSizeMB, c.LogMaxBackups)
				}
				if c.EffectiveUploadsDir() != filepath.Join(abs("files"), "uploads") {
					t.Errorf("uploads dir = %q", c.EffectiveUploadsDir())
				}
			},
		},
		{
			name: "file beats default",
			file: `,"upload_retention":"24h","retention_sweep_interval":"5m","log_max_size_mb":1,"log_max_backups":9,"uploads_dir":` + jsonStr(abs("file-up")),
			check: func(t *testing.T, c config.Config) {
				if time.Duration(c.UploadRetention) != 24*time.Hour || time.Duration(c.RetentionSweepInterval) != 5*time.Minute {
					t.Errorf("retention=%s sweep=%s", c.UploadRetention, c.RetentionSweepInterval)
				}
				if c.LogMaxSizeMB != 1 || c.LogMaxBackups != 9 || c.UploadsDir != abs("file-up") {
					t.Errorf("got %+v", c)
				}
			},
		},
		{
			name: "env beats file",
			file: `,"upload_retention":"24h","log_max_size_mb":1,"uploads_dir":` + jsonStr(abs("file-up")),
			env: map[string]string{
				"VMRC_UPLOAD_RETENTION":         "2h",
				"VMRC_RETENTION_SWEEP_INTERVAL": "10m",
				"VMRC_LOG_MAX_SIZE_MB":          "20",
				"VMRC_LOG_MAX_BACKUPS":          "5",
				"VMRC_UPLOADS_DIR":              abs("env-up"),
			},
			check: func(t *testing.T, c config.Config) {
				if time.Duration(c.UploadRetention) != 2*time.Hour || time.Duration(c.RetentionSweepInterval) != 10*time.Minute {
					t.Errorf("retention=%s sweep=%s", c.UploadRetention, c.RetentionSweepInterval)
				}
				if c.LogMaxSizeMB != 20 || c.LogMaxBackups != 5 || c.UploadsDir != abs("env-up") {
					t.Errorf("got %+v", c)
				}
			},
		},
		{
			name: "flag beats env",
			env:  map[string]string{"VMRC_UPLOAD_RETENTION": "2h", "VMRC_LOG_MAX_SIZE_MB": "20", "VMRC_UPLOADS_DIR": abs("env-up")},
			args: []string{"--upload-retention", "30m", "--retention-sweep-interval", "1m", "--log-max-size-mb", "0", "--log-max-backups", "1", "--uploads-dir", abs("flag-up")},
			check: func(t *testing.T, c config.Config) {
				if time.Duration(c.UploadRetention) != 30*time.Minute || time.Duration(c.RetentionSweepInterval) != time.Minute {
					t.Errorf("retention=%s sweep=%s", c.UploadRetention, c.RetentionSweepInterval)
				}
				if c.LogMaxSizeMB != 0 || c.LogMaxBackups != 1 || c.UploadsDir != abs("flag-up") {
					t.Errorf("got %+v", c)
				}
			},
		},
		{
			name: "flag zero disables retention",
			file: `,"upload_retention":"24h"`,
			args: []string{"--upload-retention", "0"},
			check: func(t *testing.T, c config.Config) {
				if c.UploadRetention != 0 {
					t.Errorf("retention = %s, want 0", c.UploadRetention)
				}
			},
		},
	}
	for _, cmdName := range []string{"run", "setup"} {
		flags := runCommand().Flags
		if cmdName == "setup" {
			flags = setupCommand().Flags
		}
		for _, tt := range tests {
			t.Run(cmdName+"/"+tt.name, func(t *testing.T) {
				for _, k := range []string{"VMRC_UPLOAD_RETENTION", "VMRC_RETENTION_SWEEP_INTERVAL", "VMRC_LOG_MAX_SIZE_MB", "VMRC_LOG_MAX_BACKUPS", "VMRC_UPLOADS_DIR"} {
					t.Setenv(k, "")
					_ = os.Unsetenv(k)
				}
				for k, v := range tt.env {
					t.Setenv(k, v)
				}
				c, err := resolveWith(t, flags, tt.args, "{"+baseJSON+tt.file+"}")
				if err != nil {
					t.Fatalf("resolve: %v", err)
				}
				tt.check(t, c)
			})
		}
	}
}

func TestRetentionFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"negative retention", []string{"--upload-retention", "-1h"}, "upload_retention"},
		{"negative sweep", []string{"--retention-sweep-interval", "-1s"}, "retention_sweep_interval"},
		{"negative log size", []string{"--log-max-size-mb", "-1"}, "log_max_size_mb"},
		{"relative uploads dir", []string{"--uploads-dir", "rel"}, "uploads_dir"},
	}
	base := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveWith(t, runCommand().Flags, append([]string{"--base-dir", base, "--state-dir", base}, tt.args...), `{"read_globs":["*.log"]}`)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func jsonStr(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}
