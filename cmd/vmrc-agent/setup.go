package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/pddstudio/vmrc/internal/service"
	"github.com/pddstudio/vmrc/internal/tunnel"
	"github.com/urfave/cli/v3"
)

func setupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "one-click: write config, create dirs, enroll on the tailnet, install + start the boot service",
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
		},
		Action: setupAction,
	}
}

// needEnroll decides whether the enroll step must run. With an auth key we
// always (re-)enroll; without one we only enroll when there is no state to
// resume.
func needEnroll(stateExists bool, authKey string) bool {
	if authKey != "" {
		return true
	}
	return !stateExists
}

// shouldWriteConfig reports whether a config file needs to be created at path.
func shouldWriteConfig(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

// writeDefaultConfig writes cfg (non-secret fields only; secrets are json:"-")
// to path, creating the parent directory.
func writeDefaultConfig(path string, cfg config.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, b, 0o644)
}

func setupAction(ctx context.Context, cmd *cli.Command) error {
	// 0. Privilege gate — installing a system service needs elevation.
	if err := service.RequireElevated(); err != nil {
		return err
	}

	cfg, err := resolveConfig(cmd)
	if err != nil {
		return err
	}
	if err := config.RequireAbsPaths(cfg); err != nil {
		return err
	}

	// 1. Ensure config file (don't clobber an existing one).
	cfgPath := configPath(cmd)
	if shouldWriteConfig(cfgPath) {
		if err := writeDefaultConfig(cfgPath, cfg); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Printf("wrote config %s\n", cfgPath)
	} else {
		fmt.Printf("config %s already exists; leaving it untouched\n", cfgPath)
	}

	// 2. Create required directories.
	if err := config.EnsureDirs(cfg); err != nil {
		return err
	}
	fmt.Printf("ensured dirs: state=%s base=%s\n", cfg.StateDir, cfg.BaseDir)

	// 3. Enroll on the tailnet (transient: bring the node up to persist state,
	//    then close). TS_AUTHKEY is used here and never written to disk.
	stateExists := tunnel.HasState(cfg.StateDir)
	if needEnroll(stateExists, cfg.AuthKey) {
		if cfg.AuthKey == "" && !stateExists {
			return fmt.Errorf("no tailnet state and no TS_AUTHKEY set; export TS_AUTHKEY=<tailscale auth key> and re-run")
		}
		fmt.Println("enrolling on the tailnet...")
		tun, err := tunnel.Up(ctx, tunnel.Config{
			Hostname: cfg.Hostname,
			AuthKey:  cfg.AuthKey,
			StateDir: cfg.StateDir,
			Port:     cfg.ListenPort,
		})
		if err != nil {
			return fmt.Errorf("enroll: %w", err)
		}
		fmt.Printf("enrolled as %s (reachable at %s://%s:%d once the service is up)\n",
			tun.FQDN(), tun.Scheme(), tun.FQDN(), cfg.ListenPort)
		if cerr := tun.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "warning: closing enroll tunnel: %v\n", cerr)
		}
	} else {
		fmt.Printf("tailnet state already present in %s; skipping enroll\n", cfg.StateDir)
	}

	// 4. Install the boot service (idempotent: tolerate an existing install).
	mgr, err := buildManager(cmd)
	if err != nil {
		return err
	}
	if err := mgr.Control("install"); err != nil {
		// Only swallow the error if Status confirms the service already exists;
		// otherwise it is a real install failure.
		if st, serr := mgr.Status(); serr == nil {
			fmt.Printf("service %q already installed (%s); skipping install\n", cmd.String("service-name"), st)
		} else {
			return fmt.Errorf("installing service: %w", err)
		}
	} else {
		fmt.Printf("installed service %q\n", cmd.String("service-name"))
	}

	// 5. Start it (idempotent: skip if already running).
	if st, _ := mgr.Status(); st == "running" {
		fmt.Printf("service %q already running\n", cmd.String("service-name"))
	} else if err := mgr.Control("start"); err != nil {
		return fmt.Errorf("starting service: %w", err)
	}
	fmt.Println("setup complete; the agent will rejoin the tailnet automatically on every boot.")
	return nil
}
