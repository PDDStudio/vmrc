package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/pddstudio/vmrc/internal/service"
	"github.com/pddstudio/vmrc/internal/tunnel"
	"github.com/urfave/cli/v3"
)

// serviceArgs builds the OS service launch arguments. It bakes the absolute
// config path AND the service name so the launched `run` body constructs a
// service.Manager whose identity matches the registered service. For a system
// install it also bakes --system, so the boot-launched `run` (which defaults to
// user mode) resolves system-mode defaults for any field the config file omits.
func serviceArgs(absConfigPath, serviceName string, mode config.Mode) []string {
	args := []string{"run", "--config", absConfigPath, "--service-name", serviceName}
	if mode == config.ModeSystem {
		args = append(args, "--system")
	}
	return args
}

// managedBinDir resolves the managed bin/ directory for mode WITHOUT loading or
// validating runtime config. Control actions (status/stop/uninstall) and
// executable resolution only need the service path, and must not fail because
// the config file is unreadable or carries an invalid runtime field (e.g. an
// out-of-range listen_port) — that regressed the old system-manager path, which
// never touched the config for control actions. It starts from the mode defaults
// and overlays only bin_dir from the config file when it is present and parses.
func managedBinDir(cmd *cli.Command, mode config.Mode) (string, error) {
	base, err := config.DefaultsForLayout(mode, userLayout(cmd, mode))
	if err != nil {
		return "", err
	}
	binDir := base.BinDir
	path, err := configPathForMode(cmd, mode)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		if cfg, lerr := config.LoadFile(base, path); lerr == nil && cfg.BinDir != "" {
			binDir = cfg.BinDir
		}
	}
	return binDir, nil
}

// managedExecutable returns the absolute path of the relocated agent binary for
// the given mode (the copy setup installs into <config dir>/bin). The boot
// service must be registered against this path, not os.Executable() — otherwise
// it points at wherever setup happened to run from (e.g. ~/Downloads), breaks if
// that copy moves, and never picks up updates applied to the managed copy.
func managedExecutable(cmd *cli.Command, mode config.Mode) (string, error) {
	binDir, err := managedBinDir(cmd, mode)
	if err != nil {
		return "", err
	}
	return filepath.Join(binDir, binaryFileName()), nil
}

// buildManager constructs a service.Manager whose launch arguments are
// "run --config <abs config path> --service-name <name>" and whose Executable is
// the managed bin/ copy. It reads only the config *path* (to bake into the
// service Arguments); it does not load or validate the config file. Used by the
// management commands and setup.
func buildManager(cmd *cli.Command, mode config.Mode) (*service.Manager, error) {
	p := configPath(cmd)
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, fmt.Errorf("resolving config path %q: %w", p, err)
	}
	exe, err := managedExecutable(cmd, mode)
	if err != nil {
		return nil, err
	}
	return service.New(service.Config{
		Name:        cmd.String("service-name"),
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		Executable:  exe,
		Arguments:   serviceArgs(abs, cmd.String("service-name"), mode),
	})
}

// controller is the common verb surface shared by the system service Manager and
// the user-mode Launcher.
type controller interface {
	Control(action string) error
	Status() (string, error)
}

type managerController struct{ *service.Manager }

type launcherController struct{ l service.Launcher }

func (c launcherController) Control(action string) error {
	switch action {
	case "install":
		return c.l.Install()
	case "uninstall":
		return c.l.Uninstall()
	case "start":
		return c.l.Start()
	case "stop":
		return c.l.Stop()
	}
	return fmt.Errorf("unknown action %q", action)
}
func (c launcherController) Status() (string, error) { return c.l.Status() }

// buildController returns a managerController for system mode or a launcherController
// for user mode (its spec points at the managed bin/ binary).
func buildController(cmd *cli.Command, mode config.Mode) (controller, error) {
	if mode == config.ModeSystem {
		mgr, err := buildManager(cmd, mode)
		if err != nil {
			return nil, err
		}
		return managerController{mgr}, nil
	}
	cfgPath, err := configPathForMode(cmd, mode)
	if err != nil {
		return nil, err
	}
	absCfg, err := filepath.Abs(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("resolving config path %q: %w", cfgPath, err)
	}
	exe, err := managedExecutable(cmd, mode)
	if err != nil {
		return nil, err
	}
	user, err := currentUserName()
	if err != nil {
		return nil, err
	}
	l, err := service.NewLauncher(service.LaunchSpec{
		Name:       cmd.String("service-name"),
		Executable: exe,
		Args:       serviceArgs(absCfg, cmd.String("service-name"), mode),
		User:       user,
		// Only consulted when (re)registering the Windows task: HighestAvailable
		// unless --no-elevate (which also selected the app-data layout).
		Elevated: !cmd.Bool(noElevateFlagName),
	})
	if err != nil {
		return nil, err
	}
	return launcherController{l}, nil
}

// elevatedTaskFor reports whether the Windows logon task cmd manages is (or
// will be) elevated: for setup/install unless --no-elevate, otherwise when the
// detected install uses the protected layout. Used for the privilege policy.
func elevatedTaskFor(cmd *cli.Command, mode config.Mode) bool {
	if mode != config.ModeUser {
		return false
	}
	if registersTask(cmd) {
		return !cmd.Bool(noElevateFlagName)
	}
	return userLayout(cmd, mode) == config.LayoutProtected
}

// preflightInstall checks absolute paths and that a protected install keeps
// its binary in the admin-only dir, and warns if no tsnet state exists yet (the
// enroll-before-install reminder). cfg is the resolved config. Elevation is
// checked by the caller before anything is written.
func preflightInstall(mode config.Mode, layout config.UserLayout, cfg config.Config) error {
	if err := checkProtectedBinDir(cfg, mode, layout); err != nil {
		return err
	}
	if err := config.RequireAbsPaths(cfg); err != nil {
		return err
	}
	if !tunnel.HasState(cfg.StateDir) {
		fmt.Fprintf(os.Stderr, "warning: no tailnet state in %q yet; the service cannot enroll at boot without it.\n"+
			"  Run `vmrc-agent setup` (recommended) or `vmrc-agent run` once with TS_AUTHKEY set before relying on boot-start.\n", cfg.StateDir)
	}
	return nil
}

// doInstall relocates the running binary into the managed bin/ dir — so the
// service's registered executable (the managed copy) actually exists — and then
// registers the boot service. A direct `install` must relocate too: without it,
// buildController points the service at a managed path only `setup` would have
// created, so a service registered by a bare `install` would launch a missing
// binary.
func doInstall(cfg config.Config, self string, c controller) error {
	if _, err := relocateRunningBinary(self, cfg.BinDir); err != nil {
		return fmt.Errorf("installing binary: %w", err)
	}
	return c.Control("install")
}

func installCommand() *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "register the agent as a boot service (run `setup` for one-click)",
		Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag(), noElevateFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			mode := resolveMode(cmd)
			if err := validateNoElevate(cmd, mode); err != nil {
				return err
			}
			if err := requireElevatedFor(mode, "install", elevatedTaskFor(cmd, mode)); err != nil {
				return err
			}
			if err := refuseLayoutConflict(cmd, mode); err != nil {
				return err
			}
			layout := userLayout(cmd, mode)
			mig, err := prepareInstallLocation(cmd, mode, layout)
			if err != nil {
				return err
			}
			if mig != nil {
				mig.report()
			}
			cfg, err := resolveConfigForMode(cmd, mode)
			if err != nil {
				return err
			}
			if err := preflightInstall(mode, layout, cfg); err != nil {
				return err
			}
			self, err := os.Executable()
			if err != nil {
				return err
			}
			c, err := buildController(cmd, mode)
			if err != nil {
				return err
			}
			if mig != nil {
				// End the instance still running the old, user-writable binary.
				_ = c.Control("stop")
			}
			if err := doInstall(cfg, self, c); err != nil {
				return err
			}
			if mig != nil {
				mig.cleanupLegacyBinary()
			}
			fmt.Printf("installed service %q\n", cmd.String("service-name"))
			return nil
		},
	}
}

// uninstallService stops any running instance before deregistering the service,
// so `uninstall` doesn't leave the agent process alive until the next reboot: a
// bare deregister removes the boot entry but not the live process. The stop is
// best-effort — a not-installed or already-stopped service still uninstalls
// cleanly — while a running one is brought down first.
func uninstallService(c controller) error {
	_ = c.Control("stop")
	return c.Control("uninstall")
}

func uninstallCommand() *cli.Command {
	return &cli.Command{
		Name:  "uninstall",
		Usage: "deregister the boot service",
		Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			mode := resolveMode(cmd)
			// Deleting a task needs the rights it was registered with; which
			// level a legacy %LOCALAPPDATA% install used is unknown, so Windows
			// keeps requiring elevation here.
			if err := requireElevatedFor(mode, "uninstall", mode == config.ModeUser); err != nil {
				return err
			}
			c, err := buildController(cmd, mode)
			if err != nil {
				return err
			}
			if err := uninstallService(c); err != nil {
				return err
			}
			fmt.Printf("uninstalled service %q\n", cmd.String("service-name"))
			return nil
		},
	}
}

func startCommand() *cli.Command {
	return &cli.Command{
		Name:  "start",
		Usage: "start the installed service",
		Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			mode := resolveMode(cmd)
			if err := requireElevatedFor(mode, "start", elevatedTaskFor(cmd, mode)); err != nil {
				return err
			}
			c, err := buildController(cmd, mode)
			if err != nil {
				return err
			}
			if err := c.Control("start"); err != nil {
				return err
			}
			fmt.Printf("started service %q\n", cmd.String("service-name"))
			return nil
		},
	}
}

func stopCommand() *cli.Command {
	return &cli.Command{
		Name:  "stop",
		Usage: "stop the installed service",
		Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			mode := resolveMode(cmd)
			if err := requireElevatedFor(mode, "stop", elevatedTaskFor(cmd, mode)); err != nil {
				return err
			}
			c, err := buildController(cmd, mode)
			if err != nil {
				return err
			}
			if err := c.Control("stop"); err != nil {
				return err
			}
			fmt.Printf("stopped service %q\n", cmd.String("service-name"))
			return nil
		},
	}
}

func statusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "report whether the installed service is running",
		Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			c, err := buildController(cmd, resolveMode(cmd))
			if err != nil {
				return err
			}
			st, err := c.Status()
			if err != nil {
				return err
			}
			fmt.Printf("%s: %s\n", cmd.String("service-name"), st)
			return nil
		},
	}
}
