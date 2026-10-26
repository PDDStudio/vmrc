//go:build windows

package agent

import (
	"context"
	"os/exec"
	"syscall"
)

// shellCommand wraps a command line in cmd.exe.
//
// The line is handed to cmd verbatim via SysProcAttr.CmdLine instead of as an
// exec argument: Go escapes each argument for CommandLineToArgvW (an embedded
// `"` becomes `\"`), but cmd.exe parses its own command line and does not
// understand `\"`, so `echo "a b"` would print `\"a b\"` and quoted paths would
// break. See cmdShellLine for the exact form.
func shellCommand(ctx context.Context, cmdline string) *exec.Cmd {
	c := exec.CommandContext(ctx, "cmd.exe")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdShellLine(cmdline)}
	return c
}
