//go:build windows

package service

import (
	"strings"
	"testing"
)

func TestSchtasksCreateArgs(t *testing.T) {
	spec := LaunchSpec{
		Name:       "vmrc-agent",
		Executable: `C:\Users\dev\AppData\Local\vmrc\bin\vmrc-agent.exe`,
		Args:       []string{"run", "--config", `C:\Users\dev\AppData\Local\vmrc\agent.json`},
		User:       `DESKTOP-1\dev`,
		Elevated:   true,
	}
	args := schtasksCreateArgs(spec)
	joined := strings.Join(args, " ")
	for _, want := range []string{"/Create", "/TN", "vmrc-agent", "/SC", "ONLOGON", "/RL", "HIGHEST", "/IT", "/RU", `DESKTOP-1\dev`, "/F"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("create args missing %q: %v", want, args)
		}
	}
	tr := argValue(args, "/TR")
	if !strings.Contains(tr, `"C:\Users\dev\AppData\Local\vmrc\bin\vmrc-agent.exe"`) || !strings.Contains(tr, "--config") {
		t.Fatalf("/TR malformed: %q", tr)
	}
}
