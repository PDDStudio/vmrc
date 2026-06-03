package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
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
			&cli.StringSliceFlag{Name: "allow-login", Usage: "identity allowlist: Tailscale login name (repeatable)"},
			&cli.StringSliceFlag{Name: "allow-tag", Usage: "identity allowlist: ACL tag, e.g. tag:prod (repeatable)"},
			&cli.StringFlag{Name: "token", Usage: "shared-token fallback (prefer the VMRC_TOKEN env var)", Sources: cli.EnvVars("VMRC_TOKEN")},
			&cli.StringFlag{Name: "hostname", Usage: "tsnet node name on the tailnet", Sources: cli.EnvVars("VMRC_HOSTNAME")},
			&cli.StringFlag{Name: "state-dir", Usage: "tsnet state directory", Sources: cli.EnvVars("VMRC_STATE_DIR")},
			&cli.StringFlag{Name: "log-file", Usage: "log-file path (default: <config-dir>/logs/agent.log)", Sources: cli.EnvVars("VMRC_LOG_FILE")},
			&cli.BoolFlag{Name: "local", Usage: "serve on a plain local TCP listener (dev; bypasses the tailnet tunnel)"},
		},
		Action: runAction,
	}
}

// configPath returns the effective config-file path for cmd: the --config flag
// value if set, otherwise the OS default. The single source of truth for both
// resolveConfig (which loads it) and buildManager (which bakes it into the
// service launch args).
func configPath(cmd *cli.Command) string {
	if p := cmd.String("config"); p != "" {
		return p
	}
	return config.DefaultConfigPath()
}

// configPathForMode returns the explicit --config value if set, otherwise the
// default config path for the given mode.
func configPathForMode(cmd *cli.Command, mode config.Mode) (string, error) {
	if p := cmd.String("config"); p != "" {
		return p, nil
	}
	return config.ConfigPathFor(mode)
}

// resolveConfig applies precedence flag → env → JSON file → default and reads
// the env-only secrets. Used by both run and setup.
func resolveConfig(cmd *cli.Command) (config.Config, error) {
	return resolveConfigFrom(cmd, config.Defaults(), configPath(cmd), cmd.IsSet("config"))
}

// resolveConfigForMode is resolveConfig with mode-derived defaults (used by setup
// and the management commands).
func resolveConfigForMode(cmd *cli.Command, mode config.Mode) (config.Config, error) {
	base, err := config.DefaultsFor(mode)
	if err != nil {
		return config.Config{}, err
	}
	path, err := configPathForMode(cmd, mode)
	if err != nil {
		return config.Config{}, err
	}
	return resolveConfigFrom(cmd, base, path, cmd.IsSet("config"))
}

// resolveConfigFrom applies precedence flag → env → JSON file → the given base
// defaults, reads env-only secrets, and validates.
func resolveConfigFrom(cmd *cli.Command, cfg config.Config, path string, explicit bool) (config.Config, error) {
	// Overlay the JSON file when present. A missing DEFAULT path is fine
	// (flags/env may supply everything), but a user-named --config that can't
	// be read, or any non-"not found" stat error (e.g. permission denied), is a
	// hard failure rather than a silent fall-back to defaults.
	_, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		var err error
		cfg, err = config.LoadFile(cfg, path)
		if err != nil {
			return cfg, fmt.Errorf("loading config %q: %w", path, err)
		}
	case explicit || !errors.Is(statErr, fs.ErrNotExist):
		return cfg, fmt.Errorf("config file %q: %w", path, statErr)
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
	if cmd.IsSet("allow-login") {
		cfg.AllowedLogins = cmd.StringSlice("allow-login")
	}
	if cmd.IsSet("allow-tag") {
		cfg.AllowedTags = cmd.StringSlice("allow-tag")
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
	// github token is optional and env-only; it only raises the self-update API rate limit.
	cfg.GitHubToken = os.Getenv("GITHUB_TOKEN")

	// Pin the tsnet node name when unset: derive a stable, DNS-valid name from the
	// OS hostname so the node name, MagicDNS name, and cert agree. Without this,
	// tsnet falls back to the binary basename (e.g. "vmrc-agent.exe"), whose
	// ".exe" is not a valid DNS label. An explicit --hostname / VMRC_HOSTNAME, or a
	// hostname already in the config file, takes precedence.
	if cfg.Hostname == "" {
		cfg.Hostname = config.DefaultHostname()
	}

	warns, err := config.Validate(cfg)
	if err != nil {
		return cfg, err
	}
	for _, w := range warns {
		log.Printf("WARNING: %s", w)
	}
	return cfg, nil
}

func runAction(ctx context.Context, cmd *cli.Command) error {
	// Set up file logging FIRST — before config resolution — so that even an
	// early failure (unreadable config, bad flags) is captured durably. Under a
	// service/boot supervisor stderr goes nowhere, so without this a
	// start-then-stop crash leaves no trace. A logging failure is non-fatal: warn
	// and keep going on stderr only.
	logPath := cmd.String("log-file")
	if logPath == "" {
		logPath = config.LogPath(configPath(cmd))
	}
	banner := fmt.Sprintf("vmrc-agent %s (commit %s) starting: pid=%d args=%v", version, commit, os.Getpid(), os.Args[1:])
	if err := setupLogging(logPath, banner); err != nil {
		log.Printf("WARNING: file logging to %q disabled: %v", logPath, err)
	}

	cfg, err := resolveConfig(cmd)
	if err != nil {
		return err
	}
	local := cmd.Bool("local")
	mgr, err := service.New(service.Config{
		Name:        cmd.String("service-name"),
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		// Recover panics in the serve goroutine: a panic there would otherwise
		// crash the process with the stack only on (discarded) stderr. Log it to
		// the file and surface it as an error so the supervisor records a failure.
		Run: func(c context.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("agent panic: %v", r)
					log.Printf("PANIC: %v\n%s", r, debug.Stack())
				}
			}()
			return serveAgent(c, cfg, local)
		},
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
		Capturer:  agent.ScreenCapturer{},
		BaseDir:   cfg.BaseDir,
		ReadGlobs: cfg.ReadGlobs,
		Update: agent.UpdateConfig{
			Repo:        updateRepo,
			BinaryName:  serviceName,
			Version:     version,
			GitHubToken: cfg.GitHubToken,
		},
	}
	hasAllowlist := len(cfg.AllowedLogins) > 0 || len(cfg.AllowedTags) > 0
	if cfg.Token == "" && !hasAllowlist {
		log.Printf("WARNING: no token and no identity allowlist configured; all requests will be denied")
	}

	if local {
		// --local has no tailnet, so WhoIs cannot resolve identity; the token
		// governs and any allowlist is informational only.
		if hasAllowlist {
			log.Printf("note: --local cannot resolve tailnet identity; identity allowlist is NOT enforced, token governs")
		}
		srv.Authorizer = agent.AuthorizerFor(nil, cfg.AllowedLogins, cfg.AllowedTags, cfg.Token)
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
	// Over the tailnet, identity (if an allowlist is configured) governs, with
	// the token as a fallback for peers WhoIs cannot resolve.
	srv.Authorizer = agent.AuthorizerFor(tun.LocalClient(), cfg.AllowedLogins, cfg.AllowedTags, cfg.Token)
	authMode := "token"
	if hasAllowlist {
		authMode = "identity allowlist"
	}
	log.Printf("vmrc-agent listening on %s://%s:%d (tailnet; auth=%s, base=%s, read globs=%v)",
		tun.Scheme(), tun.FQDN(), cfg.ListenPort, authMode, cfg.BaseDir, cfg.ReadGlobs)
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
