// Command vmrc-agent runs the VM-side control agent: it serves the identity-gated
// HTTP API on the tailnet (plain TCP in P0; embedded tsnet from P1). It also
// manages its own boot service (added in P2).
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pddstudio/vmrc/internal/agent"
	"github.com/pddstudio/vmrc/internal/config"
	"github.com/pddstudio/vmrc/internal/tunnel"
	"github.com/urfave/cli/v3"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := &cli.Command{
		Name:  "vmrc-agent",
		Usage: "VM-side remote-control agent",
		Commands: []*cli.Command{
			runCommand(),
		},
	}
	if err := cmd.Run(ctx, os.Args); err != nil {
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
			&cli.StringFlag{
				Name:    "hostname",
				Usage:   "tsnet node name on the tailnet",
				Sources: cli.EnvVars("VMRC_HOSTNAME"),
			},
			&cli.StringFlag{
				Name:    "state-dir",
				Usage:   "tsnet state directory",
				Sources: cli.EnvVars("VMRC_STATE_DIR"),
			},
			&cli.BoolFlag{
				Name:  "local",
				Usage: "serve on a plain local TCP listener (dev; bypasses the tailnet tunnel)",
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
	if cmd.IsSet("hostname") {
		cfg.Hostname = cmd.String("hostname")
	}
	if cmd.IsSet("state-dir") {
		cfg.StateDir = cmd.String("state-dir")
	}
	// authkey is a secret: env only, never a flag or JSON key.
	cfg.AuthKey = os.Getenv("TS_AUTHKEY")

	srv := &agent.Server{
		Token:     cfg.Token,
		Capturer:  agent.ScreenCapturer{},
		BaseDir:   cfg.BaseDir,
		ReadGlobs: cfg.ReadGlobs,
	}

	if cfg.Token == "" {
		log.Printf("WARNING: no token configured; all requests will be denied until VMRC_TOKEN is set")
	}

	// --local: plain loopback TCP, no tunnel (dev escape hatch).
	if cmd.Bool("local") {
		addr := fmt.Sprintf("127.0.0.1:%d", cfg.ListenPort)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		log.Printf("vmrc-agent listening on http://%s (local; base=%s, read globs=%v)", addr, cfg.BaseDir, cfg.ReadGlobs)
		return serve(ctx, srv.Handler(), ln, nil)
	}

	// Default: bring up the embedded tsnet tunnel and serve on the tailnet.
	tun, err := tunnel.Up(ctx, tunnel.Config{
		Hostname: cfg.Hostname,
		AuthKey:  cfg.AuthKey,
		StateDir: cfg.StateDir,
		Port:     cfg.ListenPort,
	})
	if err != nil {
		return fmt.Errorf("bringing up tunnel: %w", err)
	}
	log.Printf("vmrc-agent listening on %s://%s:%d (tailnet; base=%s, read globs=%v)",
		tun.Scheme(), tun.FQDN(), cfg.ListenPort, cfg.BaseDir, cfg.ReadGlobs)
	return serve(ctx, srv.Handler(), tun.Listener(), tun.Close)
}

// serve runs h on ln until ctx is canceled, then shuts the HTTP server down
// gracefully (10s budget) and, if closeTunnel is non-nil, tears down the
// tailnet node. This is the foreground behavior the P2 service supervisor will
// invoke.
func serve(ctx context.Context, h http.Handler, ln net.Listener, closeTunnel func() error) error {
	httpSrv := &http.Server{Handler: h}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	select {
	case err := <-errCh:
		if closeTunnel != nil {
			_ = closeTunnel()
		}
		return err
	case <-ctx.Done():
		log.Printf("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := httpSrv.Shutdown(shutdownCtx)
		if closeTunnel != nil {
			if cerr := closeTunnel(); cerr != nil && err == nil {
				err = cerr
			}
		}
		return err
	}
}
