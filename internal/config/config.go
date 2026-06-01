// Package config defines the vmrc-agent configuration and its loader. Precedence
// (applied by the entrypoint) is flag → env → JSON file → compiled default.
// Secrets are excluded from the JSON file (json:"-"); they come from env.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// Config holds the agent's runtime configuration.
type Config struct {
	Hostname   string   `json:"hostname"`    // tsnet node name (used from P1)
	StateDir   string   `json:"state_dir"`   // tsnet state dir (used from P1)
	BaseDir    string   `json:"base_dir"`    // root for exec cwd, get/put, read
	ReadGlobs  []string `json:"read_globs"`  // allowlist of readable path globs
	ListenPort int      `json:"listen_port"` // HTTP listen port

	// Secrets — never loaded from or written to the JSON file.
	Token   string `json:"-"` // shared-token fallback (env VMRC_TOKEN)
	AuthKey string `json:"-"` // tailnet auth key (env TS_AUTHKEY; used from P1)
}

// paths bundles the per-OS default filesystem locations.
type paths struct {
	ConfigPath string
	StateDir   string
	BaseDir    string
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
