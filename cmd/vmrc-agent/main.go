// Command vmrc-agent runs the VM-side control agent: it serves the identity-gated
// HTTP API on the tailnet (plain TCP in P0; embedded tsnet from P1). It also
// manages its own boot service (added in P2).
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/pddstudio/vmrc/internal/agent"
	"github.com/pddstudio/vmrc/internal/config"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:  "vmrc-agent",
		Usage: "VM-side remote-control agent",
		Commands: []*cli.Command{
			runCommand(),
		},
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func runCommand() *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "run the agent in the foreground",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Usage:   "path to a JSON config file",
				Sources: cli.EnvVars("VMRC_CONFIG"),
			},
			&cli.StringFlag{
				Name:    "base-dir",
				Usage:   "root for exec cwd, get/put and read",
				Sources: cli.EnvVars("VMRC_BASE_DIR"),
			},
			&cli.IntFlag{
				Name:    "listen-port",
				Usage:   "HTTP listen port",
				Sources: cli.EnvVars("VMRC_LISTEN_PORT"),
			},
			&cli.StringSliceFlag{
				Name:  "read-glob",
				Usage: "allowlist glob for /read (repeatable)",
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "shared-token fallback (prefer the VMRC_TOKEN env var)",
				Sources: cli.EnvVars("VMRC_TOKEN"),
			},
			&cli.BoolFlag{
				Name:  "local",
				Usage: "serve on a plain local TCP listener (the only mode in P0)",
			},
		},
		Action: runAction,
	}
}

func runAction(ctx context.Context, cmd *cli.Command) error {
	// Precedence: defaults → JSON file → env → flags.
	cfg, err := config.LoadFile(config.Defaults(), cmd.String("config"))
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cmd.IsSet("base-dir") {
		cfg.BaseDir = cmd.String("base-dir")
	}
	if cmd.IsSet("listen-port") {
		cfg.ListenPort = cmd.Int("listen-port")
	}
	if cmd.IsSet("read-glob") {
		cfg.ReadGlobs = cmd.StringSlice("read-glob")
	}
	if cmd.IsSet("token") {
		cfg.Token = cmd.String("token")
	}

	srv := &agent.Server{
		Token:     cfg.Token,
		Capturer:  agent.ScreenCapturer{},
		BaseDir:   cfg.BaseDir,
		ReadGlobs: cfg.ReadGlobs,
	}

	if cfg.Token == "" {
		log.Printf("WARNING: no token configured; all requests will be denied until VMRC_TOKEN is set")
	}

	// P0 serves a plain loopback TCP listener; --local is the only mode for now.
	// P1 replaces this with the embedded tsnet listener.
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.ListenPort)
	log.Printf("vmrc-agent listening on http://%s (base=%s, read globs=%v)", addr, cfg.BaseDir, cfg.ReadGlobs)
	// TODO(P2): use http.Server.Shutdown(ctx) for graceful stop under the service manager.
	return http.ListenAndServe(addr, srv.Handler())
}
