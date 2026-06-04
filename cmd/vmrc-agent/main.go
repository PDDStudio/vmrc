// Command vmrc-agent runs the VM-side control agent: it serves the
// identity-gated HTTP API on the tailnet (embedded tsnet; plain TCP via
// --local) and manages its own boot service (install/uninstall/start/stop/
// status) plus a one-click setup.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/pddstudio/vmrc/internal/buildinfo"

	// Blank import to link the Windows application manifest (.syso resources in
	// the winres package). The manifest declares supportedOS compatibility so
	// tsnet's wingoes dependency does not panic on an "incoherent" OS version at
	// startup; see package winres. No-op on non-Windows builds.
	_ "github.com/pddstudio/vmrc/cmd/vmrc-agent/winres"
)

const (
	serviceName        = "vmrc-agent"
	serviceDisplayName = "vmrc agent"
	serviceDescription = "vmrc VM remote-control agent"
)

// Build stamps, set via -ldflags at release time (see .goreleaser.yaml). Plain
// `go build` / `go install` leave the defaults; buildinfo.Resolve then fills
// them in from the Go module build info where possible.
var (
	version = buildinfo.DevVersion
	commit  = buildinfo.NoCommit
	date    = buildinfo.NoDate
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	v, c, d := buildinfo.Resolve(version, commit, date)
	cmd := &cli.Command{
		Name:    "vmrc-agent",
		Usage:   "VM-side remote-control agent",
		Version: fmt.Sprintf("%s (commit %s, built %s)", v, c, d),
		Commands: []*cli.Command{
			runCommand(),
			setupCommand(),
			installCommand(),
			uninstallCommand(),
			startCommand(),
			stopCommand(),
			statusCommand(),
			updateCommand(),
			captureHelperCommand(),
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
