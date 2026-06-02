package main

import (
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/urfave/cli/v3"
)

func TestUpdateUsesControllerForMode(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()}}
	c, err := buildController(cmd, config.ModeUser)
	if err != nil {
		t.Fatalf("buildController: %v", err)
	}
	if _, ok := c.(launcherController); !ok {
		t.Fatalf("update should drive a launcher controller in user mode, got %T", c)
	}
}
