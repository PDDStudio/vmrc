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
// service.Manager whose identity matches the registered service.
func serviceArgs(absConfigPath, serviceName string) []string {
	return []string{"run", "--config", absConfigPath, "--service-name", serviceName}
}

// buildManager constructs a service.Manager whose launch arguments are
// "run --config <abs config path> --service-name <name>". It reads only the
// config *path* (to bake into the service Arguments); it does not load or
// validate the config file. Used by the management commands and setup.
func buildManager(cmd *cli.Command) (*service.Manager, error) {
	p := configPath(cmd)
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, fmt.Errorf("resolving config path %q: %w", p, err)
	}
	return service.New(service.Config{
		Name:        cmd.String("service-name"),
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		Arguments:   serviceArgs(abs, cmd.String("service-name")),
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
		mgr, err := buildManager(cmd)
		if err != nil {
			return nil, err
		}
		return managerController{mgr}, nil
	}
	cfg, err := resolveConfigForMode(cmd, mode)
	if err != nil {
		return nil, err
	}
	cfgPath, err := configPathForMode(cmd, mode)
	if err != nil {
		return nil, err
	}
	absCfg, err := filepath.Abs(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("resolving config path %q: %w", cfgPath, err)
	}
	exe := filepath.Join(cfg.BinDir, binaryFileName())
	user, err := currentUserName()
	if err != nil {
		return nil, err
	}
	l, err := service.NewLauncher(service.LaunchSpec{
		Name:       cmd.String("service-name"),
		Executable: exe,
		Args:       serviceArgs(absCfg, cmd.String("service-name")),
		User:       user,
		Elevated:   true,
	})
	if err != nil {
		return nil, err
	}
	return launcherController{l}, nil
}

// preflightInstall checks elevation (per mode) and absolute paths, and warns if
// no tsnet state exists yet (the enroll-before-install reminder). cfg is the
// resolved config.
func preflightInstall(mode config.Mode, cfg config.Config) error {
	if err := requireElevatedFor(mode, "install"); err != nil {
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

func installCommand() *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "register the agent as a boot service (run `setup` for one-click)",
		Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			mode := resolveMode(cmd)
			cfg, err := resolveConfigForMode(cmd, mode)
			if err != nil {
				return err
			}
			if err := preflightInstall(mode, cfg); err != nil {
				return err
			}
			c, err := buildController(cmd, mode)
			if err != nil {
				return err
			}
			if err := c.Control("install"); err != nil {
				return err
			}
			fmt.Printf("installed service %q\n", cmd.String("service-name"))
			return nil
		},
	}
}

func uninstallCommand() *cli.Command {
	return &cli.Command{
		Name:  "uninstall",
		Usage: "deregister the boot service",
		Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			mode := resolveMode(cmd)
			if err := requireElevatedFor(mode, "uninstall"); err != nil {
				return err
			}
			c, err := buildController(cmd, mode)
			if err != nil {
				return err
			}
			if err := c.Control("uninstall"); err != nil {
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
			if err := requireElevatedFor(mode, "start"); err != nil {
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
			if err := requireElevatedFor(mode, "stop"); err != nil {
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
