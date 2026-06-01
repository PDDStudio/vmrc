// Command vmrc-agent runs the VM-side control agent: it serves the
// identity-gated HTTP API on the tailnet (embedded tsnet; plain TCP via
// --local) and manages its own boot service (install/uninstall/start/stop/
// status) plus a one-click setup.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"
)

const (
	serviceName        = "vmrc-agent"
	serviceDisplayName = "vmrc agent"
	serviceDescription = "vmrc VM remote-control agent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := &cli.Command{
		Name:  "vmrc-agent",
		Usage: "VM-side remote-control agent",
		Commands: []*cli.Command{
			runCommand(),
			installCommand(),
			uninstallCommand(),
			startCommand(),
			stopCommand(),
			statusCommand(),
		},
	}
	if err := cmd.Run(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}

// configFlag is the shared --config flag used by every command.
func configFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "config",
		Usage:   "path to a JSON config file (default: OS system path)",
		Sources: cli.EnvVars("VMRC_CONFIG"),
	}
}

// serviceNameFlag is the shared --service-name flag.
func serviceNameFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  "service-name",
		Usage: "OS service name",
		Value: serviceName,
	}
}
