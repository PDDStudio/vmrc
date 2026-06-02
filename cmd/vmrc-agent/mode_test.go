package main

import (
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/urfave/cli/v3"
)

func TestResolveModeDefaultsToUser(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{systemFlag()}}
	if got := resolveMode(cmd); got != config.ModeUser {
		t.Fatalf("default mode = %q, want user", got)
	}
}
