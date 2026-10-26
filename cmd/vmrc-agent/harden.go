package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/pddstudio/vmrc/internal/service"
	"github.com/urfave/cli/v3"
)

// systemRootNoDescend are the SystemRoot subdirectories SecureDir resets but
// does not walk into: "users" holds the separately protected per-user roots,
// "files" is the (possibly large) system-mode base dir, which plain
// inheritance from the root already covers.
var systemRootNoDescend = []string{"users", "files"}

// secureSystemRoot creates (if needed) and locks down C:\ProgramData\vmrc:
// SYSTEM + Administrators only. No-op off Windows.
func secureSystemRoot() error {
	root := config.SystemRoot()
	if err := config.SecureDir(root, "", systemRootNoDescend); err != nil {
		return fmt.Errorf("securing %s: %w", root, err)
	}
	return nil
}

// secureProtectedUserRoot creates and locks down the current account's
// protected per-user root: SYSTEM + Administrators full control, the account
// itself read+execute only.
func secureProtectedUserRoot() (string, error) {
	root, err := config.ProtectedUserRoot()
	if err != nil {
		return "", err
	}
	sid, err := config.CurrentUserSID()
	if err != nil {
		return "", err
	}
	if err := config.SecureDir(root, sid, nil); err != nil {
		return "", fmt.Errorf("securing %s: %w", root, err)
	}
	return root, nil
}

// migration records a move of a pre-hardening Windows user-mode install
// (config + binary in %LOCALAPPDATA%\vmrc) to the protected layout.
type migration struct {
	From, To      string   // legacy and new config path
	OldBinDir     string   // legacy managed-binary dir, cleaned up afterwards
	AllowedLogins []string // carried-over allowlist, echoed for review
	AllowedTags   []string
	Note          string // non-fatal problem worth telling the operator
}

// prepareInstallLocation readies the directories setup/install write into,
// before any config is read or written. On Windows: system mode secures
// C:\ProgramData\vmrc; the protected user layout secures it and the per-user
// root, then migrates a legacy %LOCALAPPDATA% config (unless --config names a
// config explicitly). Returns the migration performed, if any. No-op elsewhere.
func prepareInstallLocation(cmd *cli.Command, mode config.Mode, layout config.UserLayout) (*migration, error) {
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	if mode == config.ModeSystem {
		return nil, secureSystemRoot()
	}
	if layout != config.LayoutProtected {
		return nil, nil
	}
	if err := secureSystemRoot(); err != nil {
		return nil, err
	}
	if _, err := secureProtectedUserRoot(); err != nil {
		return nil, err
	}
	if cmd.IsSet("config") {
		return nil, nil
	}
	legacy, err := config.ConfigPathForLayout(config.ModeUser, config.LayoutAppData)
	if err != nil {
		return nil, err
	}
	target, err := config.ConfigPathForLayout(config.ModeUser, config.LayoutProtected)
	if err != nil {
		return nil, err
	}
	defaults, err := config.DefaultsForLayout(config.ModeUser, config.LayoutProtected)
	if err != nil {
		return nil, err
	}
	return migrateLegacyUserConfig(legacy, target, defaults.BinDir)
}

// refuseLayoutConflict stops `--no-elevate` from silently coexisting with an
// existing protected (elevated) install of the same account: management
// commands would keep finding the protected config first.
func refuseLayoutConflict(cmd *cli.Command, mode config.Mode) error {
	if runtime.GOOS != "windows" || mode != config.ModeUser || !cmd.Bool(noElevateFlagName) || cmd.IsSet("config") {
		return nil
	}
	p, err := config.ConfigPathForLayout(config.ModeUser, config.LayoutProtected)
	if err != nil {
		return nil
	}
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("an elevated install exists (%s); from an elevated shell run `vmrc-agent uninstall` and delete %s before reinstalling with --no-elevate",
			p, filepath.Dir(p))
	}
	return nil
}

// migrateLegacyUserConfig copies the legacy config at legacy to target with
// bin_dir rewritten to binDir (every other key preserved verbatim), then renames
// the legacy file to <legacy>.migrated. It does nothing (nil, nil) when target
// already exists or legacy does not.
func migrateLegacyUserConfig(legacy, target, binDir string) (*migration, error) {
	if _, err := os.Stat(target); err == nil {
		return nil, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("checking %s: %w", target, err)
	}
	b, err := os.ReadFile(legacy)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading legacy config %s: %w", legacy, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil, fmt.Errorf("parsing legacy config %s: %w", legacy, err)
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	m := &migration{From: legacy, To: target, OldBinDir: filepath.Join(filepath.Dir(legacy), "bin")}
	if raw, ok := fields["bin_dir"]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			m.OldBinDir = s
		}
	}
	_ = json.Unmarshal(fields["allowed_logins"], &m.AllowedLogins)
	_ = json.Unmarshal(fields["allowed_tags"], &m.AllowedTags)

	nb, err := json.Marshal(binDir)
	if err != nil {
		return nil, err
	}
	fields["bin_dir"] = nb
	out, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding migrated config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(target, out, 0o644); err != nil {
		return nil, fmt.Errorf("writing migrated config %s: %w", target, err)
	}
	if err := os.Rename(legacy, legacy+".migrated"); err != nil {
		m.Note = fmt.Sprintf("could not rename the old config %s (%v); it is no longer used and can be deleted", legacy, err)
	}
	return m, nil
}

// report prints what a migration did, including the carried-over allowlist:
// the old config was writable by any process of the user, so it deserves a look.
func (m *migration) report() {
	fmt.Printf("migrated config %s -> %s (admin-only location)\n", m.From, m.To)
	fmt.Printf("  review the carried-over allowlist: allowed_logins=%v allowed_tags=%v\n", m.AllowedLogins, m.AllowedTags)
	if m.Note != "" {
		fmt.Printf("  note: %s\n", m.Note)
	}
}

// cleanupLegacyBinary removes the old user-writable agent binary after the
// task has been re-pointed at the protected copy. Best effort: the old process
// may still be exiting.
func (m *migration) cleanupLegacyBinary() {
	old := filepath.Join(m.OldBinDir, binaryFileName())
	if err := os.Remove(old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Printf("note: could not remove the old binary %s (%v); it is no longer used and can be deleted\n", old, err)
	}
}

// checkProtectedBinDir refuses a protected-layout install whose bin_dir points
// outside the admin-only root: the elevated task must never launch a binary
// from a user-writable directory.
func checkProtectedBinDir(cfg config.Config, mode config.Mode, layout config.UserLayout) error {
	if runtime.GOOS != "windows" || mode != config.ModeUser || layout != config.LayoutProtected {
		return nil
	}
	root, err := config.ProtectedUserRoot()
	if err != nil {
		return err
	}
	if !config.IsWithin(cfg.BinDir, root) {
		return fmt.Errorf("bin_dir %q is outside the admin-only directory %s: the elevated logon task must not launch a "+
			"binary from a location the user can write. Remove bin_dir from the config (or set it under %s), or install with --no-elevate",
			cfg.BinDir, root, root)
	}
	return nil
}

// startupHardening re-applies the Windows directory protection when the agent
// starts (repairing installs made before it existed, or ACLs changed since) and
// returns warnings to log once logging is up. Repairs need an elevated token;
// without one it does nothing. No-op off Windows.
func startupHardening(cmd *cli.Command) []string {
	if runtime.GOOS != "windows" || !service.IsElevated() {
		return nil
	}
	var warns []string
	mode := resolveMode(cmd)
	if mode == config.ModeSystem {
		if _, err := os.Stat(config.SystemRoot()); err == nil {
			if err := secureSystemRoot(); err != nil {
				warns = append(warns, fmt.Sprintf("repairing permissions: %v", err))
			}
		}
		return warns
	}
	cfgPath, err := configPathForMode(cmd, mode)
	if err != nil {
		return nil
	}
	root, err := config.ProtectedUserRoot()
	if err != nil {
		return nil
	}
	legacy, _ := config.ConfigPathForLayout(config.ModeUser, config.LayoutAppData)
	switch {
	case config.IsWithin(cfgPath, root):
		if _, err := secureProtectedUserRoot(); err != nil {
			warns = append(warns, fmt.Sprintf("repairing permissions: %v", err))
		}
	case legacy != "" && strings.EqualFold(filepath.Clean(cfgPath), filepath.Clean(legacy)):
		warns = append(warns, fmt.Sprintf("running elevated with config %s in a user-writable location: any process of this "+
			"user can change what this agent runs elevated. Re-run `vmrc-agent setup` from an elevated shell to move the "+
			"config and binary to %s, or reinstall with --no-elevate", cfgPath, root))
	}
	return warns
}
