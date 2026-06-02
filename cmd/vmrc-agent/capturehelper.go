package main

import (
	"context"
	"os"

	"github.com/pddstudio/vmrc/internal/agent"
	"github.com/urfave/cli/v3"
)

// captureHelperCommand is an internal, hidden subcommand. The session-0 service
// spawns it into the active console session; it captures the screen and writes
// the PNG to stdout. Not intended for direct use.
func captureHelperCommand() *cli.Command {
	return &cli.Command{
		Name:   "capture-helper",
		Usage:  "internal: capture the screen to stdout (spawned by the service)",
		Hidden: true,
		Action: func(_ context.Context, _ *cli.Command) error {
			png, err := agent.CaptureDirect()
			if err != nil {
				return err
			}
			_, err = os.Stdout.Write(png)
			return err
		},
	}
}
