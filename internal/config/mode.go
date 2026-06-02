package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Mode selects where the agent stores its files and how it autostarts.
type Mode string

const (
	// ModeUser runs the agent in the logged-in user's session (default).
	ModeUser Mode = "user"
	// ModeSystem runs the agent as the OS boot service (LocalSystem on Windows).
	ModeSystem Mode = "system"
)

// userAppDataDir returns the per-user, machine-local app-data root:
// %LOCALAPPDATA% on Windows (os.UserCacheDir), ~/Library/Application Support on
// macOS, and $XDG_STATE_HOME or ~/.local/state on Linux.
func userAppDataDir() (string, error) {
	if runtime.GOOS == "windows" {
		return os.UserCacheDir() // %LocalAppData% on Windows
	}
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return x, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state"), nil
}

// DefaultsFor returns compiled-in defaults for the given mode. ModeSystem keeps
// the existing machine-wide layout; ModeUser roots everything under the per-user
// app-data dir.
func DefaultsFor(mode Mode) (Config, error) {
	if mode == ModeUser {
		base, err := userAppDataDir()
		if err != nil {
			return Config{}, fmt.Errorf("resolving user app-data dir: %w", err)
		}
		p := userPaths(runtime.GOOS, base)
		return Config{
			StateDir:   p.StateDir,
			BaseDir:    p.BaseDir,
			BinDir:     p.BinDir,
			ListenPort: 47800,
		}, nil
	}
	// system mode: reuse the legacy layout, plus a bin/ under the same root.
	sp := defaultPaths(runtime.GOOS)
	c := Defaults()
	c.BinDir = filepath.Join(filepath.Dir(sp.ConfigPath), "bin")
	return c, nil
}

// userPaths returns the user-mode layout rooted under base (the OS's per-user
// app-data dir). It is pure (base is injected) so it is unit-testable on any OS.
func userPaths(goos string, base string) paths {
	root := filepath.Join(base, "vmrc")
	return paths{
		ConfigPath: filepath.Join(root, "agent.json"),
		StateDir:   filepath.Join(root, "state"),
		BaseDir:    filepath.Join(root, "files"),
		BinDir:     filepath.Join(root, "bin"),
	}
}
