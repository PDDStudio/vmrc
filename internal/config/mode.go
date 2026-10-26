package config

import (
	"fmt"
	"os"
	"os/user"
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

// resolveUserDir picks the home directory user-mode installs should target,
// given the SUDO_USER value, a lookup function, and a fallback (normally
// os.UserHomeDir). Under sudo, os.UserHomeDir returns root's home even though a
// user-mode install should land in the invoking user's home, so the pre-sudo
// user is resolved when present. Extracted from userAppDataDir for testability.
func resolveUserDir(sudoUser string, lookup func(string) (*user.User, error), fallback func() (string, error)) (string, error) {
	if sudoUser != "" && sudoUser != "root" {
		if u, err := lookup(sudoUser); err == nil {
			return u.HomeDir, nil
		}
	}
	return fallback()
}

// underSudo reports whether the process is running under sudo for a real
// (non-root) invoking user on a non-Windows OS.
func underSudo() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	s := os.Getenv("SUDO_USER")
	return s != "" && s != "root"
}

// effectiveHomeDir returns the home directory of the user the agent is being
// installed for, honoring SUDO_USER on Unix (see resolveUserDir).
func effectiveHomeDir() (string, error) {
	return resolveUserDir(os.Getenv("SUDO_USER"), user.Lookup, os.UserHomeDir)
}

// userAppDataDir returns the per-user, machine-local app-data root:
// %LOCALAPPDATA% on Windows (os.UserCacheDir), ~/Library/Application Support on
// macOS, and $XDG_STATE_HOME or ~/.local/state on Linux. On Unix under sudo it
// targets the invoking user's home rather than root's.
func userAppDataDir() (string, error) {
	if runtime.GOOS == "windows" {
		return os.UserCacheDir() // %LocalAppData% on Windows
	}
	home, err := effectiveHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	// linux/other: honor XDG_STATE_HOME only when not under sudo — a sudo
	// environment's XDG_STATE_HOME belongs to root, not the target user.
	if !underSudo() {
		if x := os.Getenv("XDG_STATE_HOME"); x != "" {
			return x, nil
		}
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
		return withOperationalDefaults(Config{
			StateDir: p.StateDir,
			BaseDir:  p.BaseDir,
			BinDir:   p.BinDir,
		}), nil
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
