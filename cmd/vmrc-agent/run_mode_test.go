package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/urfave/cli/v3"
)

func TestRunLogPathIsModeAware(t *testing.T) {
	run := func(args []string, check func(cmd *cli.Command)) {
		t.Helper()
		cmd := &cli.Command{
			Flags:  runCommand().Flags,
			Action: func(_ context.Context, cmd *cli.Command) error { check(cmd); return nil },
		}
		if err := cmd.Run(context.Background(), args); err != nil {
			t.Fatalf("cmd.Run(%v): %v", args, err)
		}
	}
	var userPath, sysPath string
	run([]string{"vmrc-agent"}, func(cmd *cli.Command) { userPath = runLogPath(cmd) })
	run([]string{"vmrc-agent", "--system"}, func(cmd *cli.Command) { sysPath = runLogPath(cmd) })

	// A manual `run` must derive its log path from the selected mode's config
	// path, matching where `setup` put things — not always the system tree.
	if userPath == "" || userPath == sysPath {
		t.Fatalf("run log path must be mode-aware: user=%q system=%q", userPath, sysPath)
	}
	if !strings.Contains(sysPath, filepath.Dir(config.DefaultConfigPath())) {
		t.Fatalf("--system log path %q must sit under the system config dir", sysPath)
	}
}

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
