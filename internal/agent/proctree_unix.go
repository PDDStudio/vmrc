//go:build !windows

package agent

import (
	"os/exec"
	"syscall"
)

// killTreeOnCancel runs the command in its own process group and makes context
// cancellation kill the whole group, so children the shell backgrounded die
// with it instead of outliving the /exec timeout.
func killTreeOnCancel(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
