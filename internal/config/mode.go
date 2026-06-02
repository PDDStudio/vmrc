package config

import "path/filepath"

// Mode selects where the agent stores its files and how it autostarts.
type Mode string

const (
	// ModeUser runs the agent in the logged-in user's session (default).
	ModeUser Mode = "user"
	// ModeSystem runs the agent as the OS boot service (LocalSystem on Windows).
	ModeSystem Mode = "system"
)

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
