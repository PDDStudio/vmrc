// Package config defines the vmrc-agent configuration and its loader. Precedence
// (applied by the entrypoint) is flag → env → JSON file → compiled default.
// Secrets are excluded from the JSON file (json:"-"); they come from env.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/pddstudio/vmrc/internal/protocol"
)

// Config holds the agent's runtime configuration.
type Config struct {
	Hostname      string   `json:"hostname"`       // tsnet node name (used from P1)
	StateDir      string   `json:"state_dir"`      // tsnet state dir (used from P1)
	BaseDir       string   `json:"base_dir"`       // root for exec cwd, get/put, read
	BinDir        string   `json:"bin_dir"`        // managed binary dir (user mode)
	ReadGlobs     []string `json:"read_globs"`     // allowlist of readable path globs
	ListenPort    int      `json:"listen_port"`    // HTTP listen port
	AllowedLogins []string `json:"allowed_logins"` // identity allowlist: Tailscale login names (used from P3)
	AllowedTags   []string `json:"allowed_tags"`   // identity allowlist: ACL tags, e.g. tag:prod (used from P3)

	// UploadsDir is where /put writes (default: <base_dir>/uploads, see
	// EffectiveUploadsDir). Left empty in the written config so it keeps tracking
	// base_dir if that moves.
	UploadsDir string `json:"uploads_dir,omitempty"`
	// UploadRetention is how long an uploaded file is kept (by mtime) before the
	// background sweeper deletes it; 0 disables the sweeper.
	UploadRetention Duration `json:"upload_retention"`
	// RetentionSweepInterval is how often the sweeper runs (it also runs once at
	// startup). Must be positive while UploadRetention is enabled.
	RetentionSweepInterval Duration `json:"retention_sweep_interval"`
	// LogMaxSizeMB rotates the agent log once it would exceed this size; 0
	// disables rotation. LogMaxBackups is how many rotated files (agent.log.1 …)
	// are kept.
	LogMaxSizeMB  int `json:"log_max_size_mb"`
	LogMaxBackups int `json:"log_max_backups"`

	// TokenFile names a file holding the shared token (owner-only permissions
	// on Unix). It's how a boot/logon service gets a token without putting it
	// in a machine-wide environment variable or its command line. VMRC_TOKEN /
	// --token take precedence.
	TokenFile string `json:"token_file,omitempty"`

	// Secrets — never loaded from or written to the JSON file.
	Token       string `json:"-"` // shared-token fallback (env VMRC_TOKEN)
	AuthKey     string `json:"-"` // tailnet auth key (env TS_AUTHKEY; used from P1)
	GitHubToken string `json:"-"` // optional GitHub token (env GITHUB_TOKEN); only raises the self-update API rate limit
}

// paths bundles the per-OS default filesystem locations.
type paths struct {
	ConfigPath string
	StateDir   string
	BaseDir    string
	BinDir     string // managed binary dir used by user mode + relocation
}

// defaultPaths returns the conventional system locations for the given GOOS.
// They are chosen so the boot service account (LocalSystem on Windows, root on
// Linux) can always read them. Non-windows/darwin GOOS fall through to the
// Linux layout.
func defaultPaths(goos string) paths {
	switch goos {
	case "windows":
		base := `C:\ProgramData\vmrc`
		return paths{
			ConfigPath: base + `\agent.json`,
			StateDir:   base + `\state`,
			BaseDir:    base + `\files`,
		}
	case "darwin":
		base := "/Library/Application Support/vmrc"
		return paths{
			ConfigPath: filepath.Join(base, "agent.json"),
			StateDir:   filepath.Join(base, "state"),
			BaseDir:    filepath.Join(base, "files"),
		}
	default: // linux and other unixes
		return paths{
			ConfigPath: "/etc/vmrc/agent.json",
			StateDir:   "/var/lib/vmrc/state",
			BaseDir:    "/var/lib/vmrc/files",
		}
	}
}

// DefaultConfigPath is the config-file location used when --config is unset.
func DefaultConfigPath() string { return defaultPaths(runtime.GOOS).ConfigPath }

// LogPath returns the agent's log-file path, conventionally located next to the
// config file at <config-dir>/logs/agent.log. Deriving it from the config path
// (rather than the install mode) is deliberate: the boot-launched `run` body is
// given only --config, and that path is already mode-correct, so the log lands
// in the right place for both user- and system-mode installs. The one exception
// is the Linux system config dir /etc/vmrc: logs do not belong in /etc, so they
// go to /var/log/vmrc/agent.log instead.
func LogPath(configPath string) string { return logPathFor(runtime.GOOS, configPath) }

// linuxSystemConfigDir / linuxSystemLogPath are the Linux (and other non-darwin
// Unix) system-mode config dir and the log path used for configs inside it.
const (
	linuxSystemConfigDir = "/etc/vmrc"
	linuxSystemLogPath   = "/var/log/vmrc/agent.log"
)

// logPathFor is LogPath for an explicit GOOS (pure, for tests).
func logPathFor(goos, configPath string) string {
	if goos != "windows" && goos != "darwin" &&
		path.Dir(filepath.ToSlash(configPath)) == linuxSystemConfigDir {
		return filepath.FromSlash(linuxSystemLogPath)
	}
	return filepath.Join(filepath.Dir(configPath), "logs", "agent.log")
}

// Compiled-in defaults for the retention and log-rotation keys.
const (
	DefaultUploadRetention        = 7 * 24 * time.Hour
	DefaultRetentionSweepInterval = time.Hour
	DefaultLogMaxSizeMB           = 10
	DefaultLogMaxBackups          = 3
)

// withOperationalDefaults fills the mode-independent defaults (port, retention,
// log rotation) into c.
func withOperationalDefaults(c Config) Config {
	c.ListenPort = 47800
	c.UploadRetention = Duration(DefaultUploadRetention)
	c.RetentionSweepInterval = Duration(DefaultRetentionSweepInterval)
	c.LogMaxSizeMB = DefaultLogMaxSizeMB
	c.LogMaxBackups = DefaultLogMaxBackups
	return c
}

// Defaults returns the compiled-in defaults for the current OS.
func Defaults() Config {
	p := defaultPaths(runtime.GOOS)
	return withOperationalDefaults(Config{
		StateDir: p.StateDir,
		BaseDir:  p.BaseDir,
	})
}

// EffectiveUploadsDir returns the directory /put writes into: UploadsDir when
// set, otherwise <BaseDir>/uploads ("" when neither is set).
func (c Config) EffectiveUploadsDir() string {
	if c.UploadsDir != "" {
		return c.UploadsDir
	}
	if c.BaseDir == "" {
		return ""
	}
	return filepath.Join(c.BaseDir, "uploads")
}

// checkUploadsDir refuses an uploads dir whose expiry would delete files that
// weren't uploaded: a filesystem root, the user's home, or base_dir itself or
// one of its ancestors (the sweeper removes everything old under the dir).
func checkUploadsDir(cfg Config) error {
	dir := filepath.Clean(cfg.EffectiveUploadsDir())
	if dir == "" || dir == "." {
		return nil
	}
	bad := func(why string) error {
		return fmt.Errorf("uploads_dir %s is %s; upload retention would delete files under it (use a dedicated dir, or set upload_retention to 0)", dir, why)
	}
	if filepath.Dir(dir) == dir {
		return bad("a filesystem root")
	}
	if home, err := os.UserHomeDir(); err == nil && dir == filepath.Clean(home) {
		return bad("your home directory")
	}
	if base := filepath.Clean(cfg.BaseDir); cfg.BaseDir != "" {
		if rel, err := filepath.Rel(dir, base); err == nil && (rel == "." || !strings.HasPrefix(rel, "..")) {
			return bad("base_dir or a parent of it")
		}
	}
	return nil
}

// LoadFile overlays JSON from path onto cfg and returns the result. An empty
// path is a no-op. Keys absent from the JSON keep their value in cfg; secret
// fields (json:"-") are never affected.
func LoadFile(cfg Config, path string) (Config, error) {
	if path == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	for _, k := range []string{"token", "auth_key", "authkey", "github_token"} {
		if _, ok := keys[k]; ok {
			return cfg, fmt.Errorf("%s: %q: secrets are not read from the config file (use token_file, or the VMRC_TOKEN / TS_AUTHKEY / GITHUB_TOKEN env vars)", path, k)
		}
	}
	// Unknown keys are an error: a typo'd allowed_login would otherwise be
	// ignored and leave the agent denying everything.
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// ResolveToken fills Token from TokenFile when no token came from the flag or
// environment. A token file that is missing, empty, or readable by other users
// (Unix) is an error rather than a silent deny-all or a leaked secret.
func (c *Config) ResolveToken() error {
	if c.Token != "" || c.TokenFile == "" {
		return nil
	}
	info, err := os.Stat(c.TokenFile)
	if err != nil {
		return fmt.Errorf("token_file: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("token_file %s is accessible by other users (mode %s); chmod 600 it", c.TokenFile, info.Mode().Perm())
	}
	b, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return fmt.Errorf("token_file: %w", err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return fmt.Errorf("token_file %s is empty", c.TokenFile)
	}
	c.Token = tok
	return nil
}

// EnsureDirs creates the state, base and uploads directories if they are
// missing. StateDir holds tailnet keys, so it is created 0700; BaseDir is 0755;
// the uploads dir is 0750 (uploaded payloads need not be world-readable). An
// empty StateDir or BaseDir is skipped (callers that require them should
// validate first). Subject to the process umask, like any MkdirAll.
func EnsureDirs(cfg Config) error {
	if cfg.StateDir != "" {
		if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
			return fmt.Errorf("create state dir %q: %w", cfg.StateDir, err)
		}
	}
	if cfg.BaseDir != "" {
		if err := os.MkdirAll(cfg.BaseDir, 0o755); err != nil {
			return fmt.Errorf("create base dir %q: %w", cfg.BaseDir, err)
		}
	}
	if up := cfg.EffectiveUploadsDir(); up != "" {
		if err := os.MkdirAll(up, 0o750); err != nil {
			return fmt.Errorf("create uploads dir %q: %w", up, err)
		}
	}
	return nil
}

// RequireAbsPaths fails if StateDir, BaseDir or UploadsDir is set but not absolute. The boot
// service runs from a different working directory than the installer, so
// relative paths would not resolve identically.
func RequireAbsPaths(cfg Config) error {
	for _, p := range []struct{ name, val string }{
		{"state_dir", cfg.StateDir},
		{"base_dir", cfg.BaseDir},
		{"uploads_dir", cfg.UploadsDir},
	} {
		if p.val != "" && !filepath.IsAbs(p.val) {
			return fmt.Errorf("%s must be an absolute path for the boot service, got %q", p.name, p.val)
		}
	}
	return nil
}

// Validate checks a resolved Config for hard errors and soft warnings. Hard
// errors (relative paths, out-of-range port) are returned as err and should
// abort startup. Soft issues (e.g. an empty read allowlist) are returned as
// warnings the caller logs but need not fail on.
func Validate(cfg Config) (warnings []string, err error) {
	if err := RequireAbsPaths(cfg); err != nil {
		return nil, err
	}
	if cfg.ListenPort < 1 || cfg.ListenPort > 65535 {
		return nil, fmt.Errorf("listen_port must be in 1..65535, got %d", cfg.ListenPort)
	}
	if cfg.UploadRetention < 0 {
		return nil, fmt.Errorf("upload_retention must not be negative (0 disables), got %s", cfg.UploadRetention)
	}
	if cfg.RetentionSweepInterval < 0 {
		return nil, fmt.Errorf("retention_sweep_interval must not be negative, got %s", cfg.RetentionSweepInterval)
	}
	if cfg.UploadRetention > 0 && cfg.RetentionSweepInterval == 0 {
		return nil, fmt.Errorf("retention_sweep_interval must be positive while upload_retention is enabled")
	}
	if cfg.LogMaxSizeMB < 0 {
		return nil, fmt.Errorf("log_max_size_mb must not be negative (0 disables rotation), got %d", cfg.LogMaxSizeMB)
	}
	if cfg.LogMaxBackups < 0 {
		return nil, fmt.Errorf("log_max_backups must not be negative, got %d", cfg.LogMaxBackups)
	}
	if cfg.UploadRetention > 0 {
		if err := checkUploadsDir(cfg); err != nil {
			return nil, err
		}
	}
	if len(cfg.ReadGlobs) == 0 {
		warnings = append(warnings, "read_globs is empty; the /read endpoint will serve no files")
	}
	if len(cfg.AllowedLogins) == 0 && len(cfg.AllowedTags) == 0 && cfg.Token == "" && cfg.TokenFile == "" {
		warnings = append(warnings, "no allowed_logins, allowed_tags, or token configured; the agent will deny every request")
	}
	if slices.Contains(cfg.AllowedTags, protocol.AgentTag) {
		warnings = append(warnings, fmt.Sprintf("allowed_tags includes %s, the tag agents themselves carry: every agent can drive every other one", protocol.AgentTag))
	}
	return warnings, nil
}
