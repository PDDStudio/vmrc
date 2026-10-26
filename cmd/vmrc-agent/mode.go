package main

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"slices"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/pddstudio/vmrc/internal/service"
	"github.com/urfave/cli/v3"
)

// systemFlag selects the legacy LocalSystem service mode; absent → user mode.
func systemFlag() cli.Flag {
	return &cli.BoolFlag{Name: "system", Usage: "install/manage as the LocalSystem boot service (default: user-session mode)"}
}

// noElevateFlagName is defined only on the commands that register the logon
// task (setup, install); its presence on a command is how userLayout tells
// "choose the layout for a new registration" from "find the existing install".
const noElevateFlagName = "no-elevate"

// noElevateFlag opts a Windows user-mode install out of the elevated logon task.
func noElevateFlag() cli.Flag {
	return &cli.BoolFlag{
		Name: noElevateFlagName,
		Usage: "Windows user mode: register the logon task with LeastPrivilege (exec runs unelevated) and keep " +
			"config + binary in %LOCALAPPDATA%\\vmrc; the default elevated task keeps them in an admin-only dir",
	}
}

// resolveMode returns the install mode for cmd: ModeSystem if --system, else ModeUser.
func resolveMode(cmd *cli.Command) config.Mode {
	if cmd.Bool("system") {
		return config.ModeSystem
	}
	return config.ModeUser
}

// registersTask reports whether cmd is one of the commands that (re)register
// the autostart unit (it defines --no-elevate).
func registersTask(cmd *cli.Command) bool {
	for _, f := range cmd.Flags {
		if slices.Contains(f.Names(), noElevateFlagName) {
			return true
		}
	}
	return false
}

// userLayout returns the user-mode layout cmd operates on: for setup/install the
// layout a new registration uses (protected on Windows unless --no-elevate),
// for every other command the layout of the existing install. Irrelevant (and
// always LayoutAppData) for system mode and off Windows.
func userLayout(cmd *cli.Command, mode config.Mode) config.UserLayout {
	if mode != config.ModeUser {
		return config.LayoutAppData
	}
	if registersTask(cmd) {
		return config.PreferredUserLayout(cmd.Bool(noElevateFlagName))
	}
	return config.DetectUserLayout()
}

// validateNoElevate rejects --no-elevate where it has no meaning.
func validateNoElevate(cmd *cli.Command, mode config.Mode) error {
	if cmd.Bool(noElevateFlagName) && mode == config.ModeSystem {
		return errors.New("--no-elevate only applies to user mode; the --system service always runs as LocalSystem/root")
	}
	return nil
}

// binaryFileName is the agent binary filename for this OS.
func binaryFileName() string {
	if runtime.GOOS == "windows" {
		return serviceName + ".exe"
	}
	return serviceName
}

// currentUserName returns the OS account name to run the logon unit as
// (DOMAIN\user on Windows).
func currentUserName() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

// requireElevatedFor enforces the privilege policy for mode/action on this OS
// (see privilegeCheck). elevatedTask says whether the Windows logon task is
// (or will be) elevated.
func requireElevatedFor(mode config.Mode, action string, elevatedTask bool) error {
	return privilegeCheck(runtime.GOOS, mode, action, elevatedTask, os.Geteuid(), os.Getenv("SUDO_USER"), service.RequireElevated)
}

// privilegeCheck is the pure privilege policy:
//
//   - system mode: elevation for everything except status and update (update
//     requires it itself, only when it has to stop a running service);
//   - Windows user mode: elevation to register, remove, or update an elevated
//     logon task (its binary lives in an admin-only dir); a --no-elevate task
//     needs none. start/stop/status need none;
//   - macOS/Linux user mode: the unit is per-user (systemd --user / LaunchAgent
//     of the invoking account), so it must NOT run as root — under sudo the unit
//     would land in root's home and never run for the user. status is exempt.
func privilegeCheck(goos string, mode config.Mode, action string, elevatedTask bool, euid int, sudoUser string, requireElevated func() error) error {
	if mode == config.ModeSystem {
		switch action {
		case "status", "update":
			return nil
		}
		return requireElevated()
	}
	if goos == "windows" {
		switch action {
		case "setup", "install", "uninstall", "update":
			if elevatedTask {
				return requireElevated()
			}
		}
		return nil
	}
	if action == "status" || euid != 0 {
		return nil
	}
	return unixRootUserModeError(sudoUser)
}

// unixRootUserModeError explains why user mode refuses to run as root.
func unixRootUserModeError(sudoUser string) error {
	who := "the account that should run the agent"
	if sudoUser != "" && sudoUser != "root" {
		who = sudoUser
	}
	return fmt.Errorf("user mode installs a per-user service for the account running this command, so it must not run "+
		"as root or under sudo (the unit would land in root's home and never run for %[1]s).\n"+
		"  Re-run without sudo as %[1]s, or pass --system for the machine-wide boot service", who)
}
