package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/pddstudio/vmrc/internal/agent"
	"github.com/pddstudio/vmrc/internal/config"
	"github.com/pddstudio/vmrc/internal/service"
	"github.com/pddstudio/vmrc/internal/tunnel"
	"github.com/urfave/cli/v3"
)

func runCommand() *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "run the agent in the foreground (also the installed service body)",
		Flags: []cli.Flag{
			configFlag(),
			serviceNameFlag(),
			&cli.StringFlag{Name: "base-dir", Usage: "root for exec cwd, get/put and read", Sources: cli.EnvVars("VMRC_BASE_DIR")},
			&cli.IntFlag{Name: "listen-port", Usage: "HTTP listen port", Sources: cli.EnvVars("VMRC_LISTEN_PORT")},
			&cli.StringSliceFlag{Name: "read-glob", Usage: "allowlist glob for /read (repeatable)"},
			&cli.StringFlag{Name: "token", Usage: "shared-token fallback (prefer the VMRC_TOKEN env var)", Sources: cli.EnvVars("VMRC_TOKEN")},
			&cli.StringFlag{Name: "hostname", Usage: "tsnet node name on the tailnet", Sources: cli.EnvVars("VMRC_HOSTNAME")},
			&cli.StringFlag{Name: "state-dir", Usage: "tsnet state directory", Sources: cli.EnvVars("VMRC_STATE_DIR")},
			&cli.BoolFlag{Name: "local", Usage: "serve on a plain local TCP listener (dev; bypasses the tailnet tunnel)"},
		},
		Action: runAction,
	}
}

// resolveConfig applies precedence flag → env → JSON file → default and reads
// the env-only secrets. Used by both run and setup.
func resolveConfig(cmd *cli.Command) (config.Config, error) {
	path := cmd.String("config")
	if path == "" {
		path = config.DefaultConfigPath()
	}
	cfg := config.Defaults()
	// Only overlay the file if it exists; an absent default path is fine
	// (flags/env may supply everything).
	if _, statErr := os.Stat(path); statErr == nil {
		var err error
		cfg, err = config.LoadFile(cfg, path)
		if err != nil {
			return cfg, fmt.Errorf("loading config %q: %w", path, err)
		}
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
	return cfg, nil
}

func runAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := resolveConfig(cmd)
	if err != nil {
		return err
	}
	local := cmd.Bool("local")
	mgr, err := service.New(service.Config{
		Name:        cmd.String("service-name"),
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		Run:         func(c context.Context) error { return serveAgent(c, cfg, local) },
	})
	if err != nil {
		return err
	}
	// mgr.Run() blocks: interactively it serves until SIGINT/SIGTERM; under a
	// service supervisor it hooks into the supervisor lifecycle.
	return mgr.Run()
}

// serveAgent builds the agent server and serves it until ctx is canceled. It is
// the body invoked interactively, by the installed service, and (indirectly) by
// setup. It ensures the working directories exist first so a service self-heals
// a missing dir.
func serveAgent(ctx context.Context, cfg config.Config, local bool) error {
	if err := config.EnsureDirs(cfg); err != nil {
		return err
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

	if local {
		addr := fmt.Sprintf("127.0.0.1:%d", cfg.ListenPort)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		log.Printf("vmrc-agent listening on http://%s (local; base=%s, read globs=%v)", addr, cfg.BaseDir, cfg.ReadGlobs)
		return serve(ctx, srv.Handler(), ln, nil)
	}

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
// gracefully (10s budget) and, if closeTunnel is non-nil, tears down the tailnet
// node.
func serve(ctx context.Context, h http.Handler, ln net.Listener, closeTunnel func() error) error {
	httpSrv := &http.Server{Handler: h}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	select {
	case err := <-errCh:
		if closeTunnel != nil {
			if cerr := closeTunnel(); cerr != nil {
				log.Printf("tunnel close after server exit: %v", cerr)
			}
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
