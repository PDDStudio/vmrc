package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/pddstudio/vmrc/internal/service"
	"github.com/pddstudio/vmrc/internal/tunnel"
	"github.com/urfave/cli/v3"
)

// hasLegacyState reports whether stateDir holds tsnet state worth migrating.
func hasLegacyState(stateDir string) bool {
	if !tunnel.HasState(stateDir) {
		return false
	}
	entries, err := os.ReadDir(stateDir)
	return err == nil && len(entries) > 0
}

// migrateData copies the legacy working dir's state/, files/, and agent.json into
// the user working dir, preserving the tsnet node identity (state/). It does not
// delete the source; the caller uninstalls the service afterward.
func migrateData(legacyRoot, userRoot string) error {
	for _, sub := range []string{"state", "files"} {
		src := filepath.Join(legacyRoot, sub)
		if _, err := os.Stat(src); err == nil {
			if err := service.CopyTree(src, filepath.Join(userRoot, sub)); err != nil {
				return fmt.Errorf("copy %s: %w", sub, err)
			}
		}
	}
	srcCfg := filepath.Join(legacyRoot, "agent.json")
	if _, err := os.Stat(srcCfg); err == nil {
		if err := service.CopyFile(srcCfg, filepath.Join(userRoot, "agent.json")); err != nil {
			return fmt.Errorf("copy agent.json: %w", err)
		}
	}
	return nil
}

// shouldMigrate is true when a legacy install has state to move and the user
// install does not yet (idempotent: once migrated, returns false).
func shouldMigrate(legacyRoot, userRoot string) bool {
	return hasLegacyState(filepath.Join(legacyRoot, "state")) &&
		!hasLegacyState(filepath.Join(userRoot, "state"))
}

// RunMigration performs the one-time legacy(system) → user conversion: stop +
// uninstall the service, copy data (preserving node identity), register + start
// the user logon unit. Assumes elevation (checked by the caller) and that the new
// binary is already placed in the user bin/ dir.
func RunMigration(cmd *cli.Command, legacyRoot, userRoot string) error {
	sysCtl, err := buildController(cmd, config.ModeSystem)
	if err != nil {
		return err
	}
	if st, _ := sysCtl.Status(); st == "running" {
		if err := sysCtl.Control("stop"); err != nil {
			return fmt.Errorf("stopping legacy service: %w", err)
		}
	}
	if err := migrateData(legacyRoot, userRoot); err != nil {
		return err
	}
	userCtl, err := buildController(cmd, config.ModeUser)
	if err != nil {
		return err
	}
	if err := userCtl.Control("install"); err != nil {
		return fmt.Errorf("installing user logon unit: %w", err)
	}
	if err := userCtl.Control("start"); err != nil {
		return fmt.Errorf("starting user logon unit: %w", err)
	}
	_ = sysCtl.Control("uninstall") // best-effort; data left in place is harmless
	return nil
}
