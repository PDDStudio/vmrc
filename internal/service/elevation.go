// Package service wraps github.com/kardianos/service so the rest of vmrc-agent
// installs, controls, and runs itself as a system boot service without touching
// kardianos types directly. The agent core (internal/agent) never imports this
// package; cmd/vmrc-agent composes them.
package service

import "runtime"

// probeElevated reports whether the current process has the privileges needed
// to register/control a system service. Defaults to the per-OS osElevated
// implementation; tests override it.
var probeElevated = osElevated

// NotElevatedError indicates the current process lacks the privileges required
// to manage the system service.
type NotElevatedError struct {
	Hint string
}

func (e *NotElevatedError) Error() string {
	return "insufficient privileges to manage the system service: " + e.Hint
}

// IsElevated reports whether the process is elevated (Administrator token or
// SYSTEM on Windows, root elsewhere).
func IsElevated() bool { return probeElevated() }

// RequireElevated returns a *NotElevatedError unless the process is elevated.
func RequireElevated() error {
	if probeElevated() {
		return nil
	}
	return &NotElevatedError{Hint: elevationHint(runtime.GOOS)}
}

// elevationHint returns OS-specific guidance for gaining the needed privileges.
func elevationHint(goos string) string {
	switch goos {
	case "windows":
		return "re-run from an elevated (Run as Administrator) PowerShell or Command Prompt"
	default:
		return "re-run with sudo (root)"
	}
}
