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
		Flags: append([]cli.Flag{
			configFlag(),
			serviceNameFlag(),
			systemFlag(),
			&cli.StringFlag{Name: "base-dir", Usage: "root for exec cwd, get/put and read", Sources: cli.EnvVars("VMRC_BASE_DIR")},
			&cli.IntFlag{Name: "listen-port", Usage: "HTTP listen port", Sources: cli.EnvVars("VMRC_LISTEN_PORT")},
			&cli.StringSliceFlag{Name: "read-glob", Usage: "allowlist glob for /read (repeatable)"},
			&cli.StringSliceFlag{Name: "allow-login", Usage: "identity allowlist: Tailscale login name (repeatable)"},
			&cli.StringSliceFlag{Name: "allow-tag", Usage: "identity allowlist: ACL tag, e.g. tag:prod (repeatable)"},
			&cli.StringFlag{Name: "token", Usage: "shared-token fallback (prefer the VMRC_TOKEN env var)", Sources: cli.EnvVars("VMRC_TOKEN")},
			&cli.StringFlag{Name: "token-file", Usage: "file holding the shared token (chmod 600); how a service gets a token", Sources: cli.EnvVars("VMRC_TOKEN_FILE")},
			&cli.StringFlag{Name: "hostname", Usage: "tsnet node name on the tailnet", Sources: cli.EnvVars("VMRC_HOSTNAME")},
			&cli.StringFlag{Name: "state-dir", Usage: "tsnet state directory", Sources: cli.EnvVars("VMRC_STATE_DIR")},
		}, retentionFlags()...),
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

// relocateRunningBinary copies the current executable (src) into binDir as the
// canonical agent binary name and returns the managed path.
func relocateRunningBinary(src, binDir string) (string, error) {
	return service.RelocateBinary(src, binDir, binaryFileName())
}

// enrollIfNeeded brings the tsnet node up once to persist state, then closes it.
// With an auth key it always (re-)enrolls; without one it enrolls only when no
// state exists yet. TS_AUTHKEY is used here and never written to disk.
func enrollIfNeeded(ctx context.Context, cfg config.Config) error {
	stateExists := tunnel.HasState(cfg.StateDir)
	if !needEnroll(stateExists, cfg.AuthKey) {
		fmt.Printf("tailnet state already present in %s; skipping enroll\n", cfg.StateDir)
		return nil
	}
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
	fmt.Printf("enrolled as %s (reachable at %s://%s:%d once the agent is up)\n",
		tun.FQDN(), tun.Scheme(), tun.FQDN(), cfg.ListenPort)
	if cerr := tun.Close(); cerr != nil {
		fmt.Fprintf(os.Stderr, "warning: closing enroll tunnel: %v\n", cerr)
	}
	return nil
}

// checkSetupAuth refuses to install an agent that would deny every request.
// Only what the installed service will see counts: the allowlists and
// token_file persist in the config, but a VMRC_TOKEN / --token given to setup
// does not (secrets never go into the file).
func checkSetupAuth(cfg config.Config) error {
	if len(cfg.AllowedLogins) > 0 || len(cfg.AllowedTags) > 0 || cfg.TokenFile != "" {
		return nil
	}
	return fmt.Errorf("refusing to set up an agent that would deny every request: pass --allow-login <you@example.com> " +
		"(your Tailscale login), --allow-tag <tag:...>, or --token-file <path> (a VMRC_TOKEN given to setup is not persisted for the service)")
}

func setupAction(ctx context.Context, cmd *cli.Command) error {
	mode := resolveMode(cmd)

	// Elevation: system mode always; user mode needs it to register a
	// highest-privileges logon task. Either way setup installs an autostart unit.
	if err := service.RequireElevated(); err != nil {
		return err
	}

	cfg, err := resolveConfigForMode(cmd, mode)
	if err != nil {
		return err
	}
	if err := config.RequireAbsPaths(cfg); err != nil {
		return err
	}
	if err := checkSetupAuth(cfg); err != nil {
		return err
	}

	cfgPath, err := configPathForMode(cmd, mode)
	if err != nil {
		return err
	}
	if shouldWriteConfig(cfgPath) {
		if err := writeDefaultConfig(cfgPath, cfg); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Printf("wrote config %s\n", cfgPath)
	} else {
		fmt.Printf("config %s already exists; leaving it untouched\n", cfgPath)
	}

	if err := config.EnsureDirs(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	fmt.Printf("ensured dirs: state=%s base=%s uploads=%s bin=%s\n", cfg.StateDir, cfg.BaseDir, cfg.EffectiveUploadsDir(), cfg.BinDir)

	self, err := os.Executable()
	if err != nil {
		return err
	}
	managed, err := relocateRunningBinary(self, cfg.BinDir)
	if err != nil {
		return fmt.Errorf("relocating binary: %w", err)
	}
	fmt.Printf("installed binary at %s\n", managed)

	if err := enrollIfNeeded(ctx, cfg); err != nil {
		return err
	}

	c, err := buildController(cmd, mode)
	if err != nil {
		return err
	}
	if err := c.Control("install"); err != nil {
		if st, serr := c.Status(); serr == nil {
			fmt.Printf("autostart unit already present (%s); leaving it\n", st)
		} else {
			return fmt.Errorf("installing autostart unit: %w", err)
		}
	} else {
		fmt.Println("installed autostart unit")
	}
	if st, _ := c.Status(); st != "running" {
		if err := c.Control("start"); err != nil {
			return fmt.Errorf("starting autostart unit: %w", err)
		}
	}
	fmt.Println("setup complete.")
	return nil
}
