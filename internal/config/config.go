// Package config defines the vmrc-agent configuration and its loader. Precedence
// (applied by the entrypoint) is flag → env → JSON file → compiled default.
// Secrets are excluded from the JSON file (json:"-"); they come from env.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// ConfigPathFor returns the default config-file path for the given mode:
// the per-user agent.json for ModeUser, or the system default for ModeSystem.
func ConfigPathFor(mode Mode) (string, error) {
	if mode == ModeUser {
		base, err := userAppDataDir()
		if err != nil {
			return "", err
		}
		return userPaths(runtime.GOOS, base).ConfigPath, nil
	}
	return DefaultConfigPath(), nil
}

// Defaults returns the compiled-in defaults for the current OS.
func Defaults() Config {
	p := defaultPaths(runtime.GOOS)
	return Config{
		StateDir:   p.StateDir,
		BaseDir:    p.BaseDir,
		ListenPort: 47800,
	}
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
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// EnsureDirs creates the state and base directories if they are missing.
// StateDir holds tailnet keys, so it is created 0700; BaseDir is 0755. An empty
// StateDir or BaseDir is skipped (callers that require them should validate
// first). Subject to the process umask, like any MkdirAll.
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
	return nil
}

// RequireAbsPaths fails if StateDir or BaseDir is set but not absolute. The boot
// service runs from a different working directory than the installer, so
// relative paths would not resolve identically.
func RequireAbsPaths(cfg Config) error {
	for _, p := range []struct{ name, val string }{
		{"state_dir", cfg.StateDir},
		{"base_dir", cfg.BaseDir},
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
	if len(cfg.ReadGlobs) == 0 {
		warnings = append(warnings, "read_globs is empty; the /read endpoint will serve no files")
	}
	return warnings, nil
}
