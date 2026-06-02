package main

import (
	"path/filepath"
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/urfave/cli/v3"
)

func TestConfigPathForModeUserVsSystem(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{configFlag(), systemFlag()}}
	userPath, err := configPathForMode(cmd, config.ModeUser)
	if err != nil {
		t.Fatal(err)
	}
	sysPath, err := configPathForMode(cmd, config.ModeSystem)
	if err != nil {
		t.Fatal(err)
	}
	if userPath == sysPath {
		t.Fatalf("user and system config paths must differ: %q", userPath)
	}
	if filepath.Base(userPath) != "agent.json" {
		t.Fatalf("unexpected user path %q", userPath)
	}
}
