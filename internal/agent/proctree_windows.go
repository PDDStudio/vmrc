//go:build windows

package agent

import (
	"os/exec"
	"strconv"
)

// killTreeOnCancel makes context cancellation kill the command's whole process
// tree (taskkill /T), so children cmd.exe started die with it instead of
// outliving the /exec timeout.
func killTreeOnCancel(c *exec.Cmd) {
	c.Cancel = func() error {
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid))
		if err := kill.Run(); err != nil {
			return c.Process.Kill()
		}
		return nil
	}
}
