package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
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

// UserLayout selects where a user-mode install keeps its config file and the
// managed binary the logon task launches.
type UserLayout int

const (
	// LayoutAppData keeps everything under the per-user app-data dir
	// (%LOCALAPPDATA%\vmrc on Windows). It is the only layout on macOS/Linux,
	// and on Windows it is used for --no-elevate installs (a LeastPrivilege
	// task) and by installs made before the protected layout existed.
	LayoutAppData UserLayout = iota
	// LayoutProtected (Windows only) is the default for an elevated logon task:
	// because the task runs with the user's full administrator token
	// (HighestAvailable), the config file and the binary it launches live in
	// %ProgramData%\vmrc\users\<account>, which only Administrators and SYSTEM
	// can write (protected DACL, see SecureDir). Otherwise any unelevated process
	// of that user could swap the binary or edit the config and be relaunched
	// elevated within a minute. State and base dir stay per-user.
	LayoutProtected
)

func (l UserLayout) String() string {
	if l == LayoutProtected {
		return "protected"
	}
	return "app-data"
}

// SystemRoot is the machine-wide install root (the directory holding the
// system-mode agent.json), e.g. C:\ProgramData\vmrc on Windows.
func SystemRoot() string { return filepath.Dir(defaultPaths(runtime.GOOS).ConfigPath) }

// usersDirName is the subdirectory of SystemRoot holding protected per-user roots.
const usersDirName = "users"

// accountDirName turns an OS account name (DOMAIN\user on Windows) into a
// single, safe path element: the part after the last separator, with
// characters invalid in Windows file names replaced.
func accountDirName(account string) string {
	if i := strings.LastIndexAny(account, `\/`); i >= 0 {
		account = account[i+1:]
	}
	account = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, account)
	if account == "" || account == "." || account == ".." {
		return "_"
	}
	return account
}

// protectedUserRoot is the protected per-user root under systemRoot (pure).
func protectedUserRoot(systemRoot, account string) string {
	return filepath.Join(systemRoot, usersDirName, accountDirName(account))
}

// ProtectedUserRoot returns the admin-only per-user root for the current
// account, e.g. C:\ProgramData\vmrc\users\<user>.
func ProtectedUserRoot() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("resolving current user: %w", err)
	}
	return protectedUserRoot(SystemRoot(), u.Username), nil
}

// preferredUserLayout is the layout a new user-mode install uses (pure).
func preferredUserLayout(goos string, noElevate bool) UserLayout {
	if goos == "windows" && !noElevate {
		return LayoutProtected
	}
	return LayoutAppData
}

// PreferredUserLayout returns the layout `setup`/`install` register a new
// user-mode task with: protected on Windows unless --no-elevate.
func PreferredUserLayout(noElevate bool) UserLayout {
	return preferredUserLayout(runtime.GOOS, noElevate)
}

// chooseUserLayout picks the layout of an existing install (pure): on Windows a
// protected config wins, an app-data config alone means a --no-elevate or
// pre-migration install, and with neither the default (protected) applies.
func chooseUserLayout(goos string, protectedExists, legacyExists bool) UserLayout {
	if goos != "windows" {
		return LayoutAppData
	}
	if !protectedExists && legacyExists {
		return LayoutAppData
	}
	return LayoutProtected
}

// DetectUserLayout returns the layout of the current account's existing
// user-mode install (see chooseUserLayout). Always LayoutAppData off Windows.
func DetectUserLayout() UserLayout {
	if runtime.GOOS != "windows" {
		return LayoutAppData
	}
	exists := func(layout UserLayout) bool {
		p, err := ConfigPathForLayout(ModeUser, layout)
		if err != nil {
			return false
		}
		_, err = os.Stat(p)
		return err == nil
	}
	return chooseUserLayout(runtime.GOOS, exists(LayoutProtected), exists(LayoutAppData))
}

// userPathsFor is userPaths with the layout applied (pure): the protected
// layout moves the config file and bin dir under protectedRoot.
func userPathsFor(goos, appBase string, layout UserLayout, protectedRoot string) paths {
	p := userPaths(goos, appBase)
	if layout == LayoutProtected {
		p.ConfigPath = filepath.Join(protectedRoot, "agent.json")
		p.BinDir = filepath.Join(protectedRoot, "bin")
	}
	return p
}

// userPathsForLayout resolves the current account's user-mode paths.
func userPathsForLayout(layout UserLayout) (paths, error) {
	base, err := userAppDataDir()
	if err != nil {
		return paths{}, fmt.Errorf("resolving user app-data dir: %w", err)
	}
	var protected string
	if layout == LayoutProtected {
		if protected, err = ProtectedUserRoot(); err != nil {
			return paths{}, err
		}
	}
	return userPathsFor(runtime.GOOS, base, layout, protected), nil
}

// DefaultsFor returns compiled-in defaults for the given mode, using the
// detected user layout (DetectUserLayout) for ModeUser.
func DefaultsFor(mode Mode) (Config, error) {
	return DefaultsForLayout(mode, DetectUserLayout())
}

// DefaultsForLayout returns compiled-in defaults for mode. ModeSystem keeps the
// machine-wide layout (layout is ignored); ModeUser uses the given layout.
func DefaultsForLayout(mode Mode, layout UserLayout) (Config, error) {
	if mode == ModeUser {
		p, err := userPathsForLayout(layout)
		if err != nil {
			return Config{}, err
		}
		return withOperationalDefaults(Config{
			StateDir: p.StateDir,
			BaseDir:  p.BaseDir,
			BinDir:   p.BinDir,
		}), nil
	}
	// system mode: reuse the legacy layout, plus a bin/ under the same root.
	c := Defaults()
	c.BinDir = filepath.Join(SystemRoot(), "bin")
	return c, nil
}

// ConfigPathFor returns the default config-file path for mode, using the
// detected user layout for ModeUser.
func ConfigPathFor(mode Mode) (string, error) {
	return ConfigPathForLayout(mode, DetectUserLayout())
}

// ConfigPathForLayout returns the default config-file path for mode: the
// per-user agent.json (for the given layout) for ModeUser, or the system
// default for ModeSystem.
func ConfigPathForLayout(mode Mode, layout UserLayout) (string, error) {
	if mode == ModeUser {
		p, err := userPathsForLayout(layout)
		if err != nil {
			return "", err
		}
		return p.ConfigPath, nil
	}
	return DefaultConfigPath(), nil
}

// IsWithin reports whether path is root or lies inside it, lexically after
// cleaning (filepath.Rel compares case-insensitively on Windows).
func IsWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
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
