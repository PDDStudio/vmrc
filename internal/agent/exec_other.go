//go:build !windows

package agent

import (
	"context"
	"os/exec"
)

// shellCommand wraps a command line in the platform shell (sh -c).
func shellCommand(ctx context.Context, cmdline string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", cmdline)
}
