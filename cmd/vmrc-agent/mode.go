package main

import (
	"os/user"
	"runtime"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/pddstudio/vmrc/internal/service"
	"github.com/urfave/cli/v3"
)

// systemFlag selects the legacy LocalSystem service mode; absent → user mode.
func systemFlag() cli.Flag {
	return &cli.BoolFlag{Name: "system", Usage: "install/manage as the LocalSystem boot service (default: user-session mode)"}
}

// resolveMode returns the install mode for cmd: ModeSystem if --system, else ModeUser.
func resolveMode(cmd *cli.Command) config.Mode {
	if cmd.Bool("system") {
		return config.ModeSystem
	}
	return config.ModeUser
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

// requireElevatedFor enforces elevation only where the mode/action needs it:
// system mode always; user mode only for install/uninstall (which register a
// highest-privileges logon task). user-mode start/stop/status need none.
func requireElevatedFor(mode config.Mode, action string) error {
	if mode == config.ModeSystem {
		return service.RequireElevated()
	}
	switch action {
	case "install", "uninstall":
		return service.RequireElevated()
	}
	return nil
}
