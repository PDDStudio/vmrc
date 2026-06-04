// Command vmrc is the host-side fleet CLI: it discovers vmrc agents on the
// tailnet (via the host's system Tailscale) and drives them over HTTP
// (ls/exec/shot/get/put).
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/pddstudio/vmrc/internal/buildinfo"
	"github.com/pddstudio/vmrc/internal/host"
	"github.com/urfave/cli/v3"
	"tailscale.com/client/local"
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
		Name:    "vmrc",
		Usage:   "drive a fleet of vmrc agents over the tailnet",
		Version: fmt.Sprintf("%s (commit %s, built %s)", v, c, d),
		Flags:   globalFlags(),
		Commands: []*cli.Command{
			lsCommand(),
			execCommand(),
			shotCommand(),
			clickCommand(),
			typeCommand(),
			keyCommand(),
			getCommand(),
			putCommand(),
			updateCommand(),
		},
	}
	// cli.Exit-wrapped errors carry an exit code and are handled by cli's default
	// exiter; other errors abort with status 1. Drop log's timestamp prefix so
	// CLI errors read cleanly (e.g. "vmrc: no agent named ...").
	log.SetFlags(0)
	log.SetPrefix("vmrc: ")
	if err := cmd.Run(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}

func globalFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "token", Usage: "shared-token fallback (prefer the VMRC_TOKEN env var)", Sources: cli.EnvVars("VMRC_TOKEN")},
		&cli.IntFlag{Name: "port", Usage: "agent HTTP port", Value: 47800},
		&cli.StringFlag{Name: "scheme", Usage: "agent URL scheme (https default; pass http for agents on a tailnet without HTTPS certs)", Value: "https"},
		&cli.StringFlag{Name: "tag", Usage: "tailnet tag identifying vmrc agents", Value: host.DefaultTag},
		&cli.DurationFlag{Name: "timeout", Usage: "per-request client timeout (0 = none; exec --all defaults to 11m to bound hung nodes)", Value: 0},
	}
}

// discoverer builds a Discoverer over the host's system tailscaled.
func discoverer() host.Discoverer {
	return host.Discoverer{Source: &local.Client{}}
}

// clientFor builds a verb client for one agent from the global flags.
func clientFor(cmd *cli.Command, a host.Agent) *host.Client {
	return &host.Client{
		BaseURL: fmt.Sprintf("%s://%s:%d", cmd.String("scheme"), a.DNSName, cmd.Int("port")),
		Token:   cmd.String("token"),
		HTTP:    &http.Client{Timeout: cmd.Duration("timeout")},
	}
}
