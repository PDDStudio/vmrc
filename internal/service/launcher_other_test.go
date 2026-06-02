//go:build !windows

package service

import "testing"

func TestUnixLauncherBuildsUserService(t *testing.T) {
	l, err := newOSLauncher(LaunchSpec{
		Name:       "vmrc-agent",
		Executable: "/home/dev/.local/state/vmrc/bin/vmrc-agent",
		Args:       []string{"run", "--config", "/home/dev/.local/state/vmrc/agent.json"},
	})
	if err != nil {
		t.Fatalf("newOSLauncher: %v", err)
	}
	st, err := l.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st != "running" && st != "stopped" && st != "unknown" {
		t.Fatalf("unexpected status %q", st)
	}
}
