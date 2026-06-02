//go:build windows

package service

import (
	"fmt"
	"os/exec"
	"strings"
)

func schtasksCreateArgs(s LaunchSpec) []string {
	// Build the /TR command string with literal double-quotes around the exe and
	// each arg. We must NOT use %q here: %q produces a Go string literal, which
	// doubles backslashes and would corrupt Windows paths (C:\...\x.exe).
	// Per-arg quoting also keeps paths with spaces (e.g. a username with a space
	// in %LOCALAPPDATA%) intact.
	quoted := make([]string, 0, len(s.Args)+1)
	quoted = append(quoted, `"`+s.Executable+`"`)
	for _, a := range s.Args {
		quoted = append(quoted, `"`+a+`"`)
	}
	tr := strings.Join(quoted, " ")
	args := []string{
		"/Create", "/TN", s.Name, "/TR", tr,
		"/SC", "ONLOGON", "/RU", s.User, "/IT", "/F",
	}
	if s.Elevated {
		args = append(args, "/RL", "HIGHEST")
	}
	return args
}

func schtasksRunArgs(name string) []string    { return []string{"/Run", "/TN", name} }
func schtasksEndArgs(name string) []string    { return []string{"/End", "/TN", name} }
func schtasksDeleteArgs(name string) []string { return []string{"/Delete", "/TN", name, "/F"} }
func schtasksQueryArgs(name string) []string  { return []string{"/Query", "/TN", name, "/FO", "LIST", "/V"} }

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func runSchtasks(args ...string) (string, error) {
	out, err := exec.Command("schtasks.exe", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("schtasks %v: %w: %s", args, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type winLauncher struct{ spec LaunchSpec }

func (l winLauncher) Install() error   { _, err := runSchtasks(schtasksCreateArgs(l.spec)...); return err }
func (l winLauncher) Uninstall() error { _, err := runSchtasks(schtasksDeleteArgs(l.spec.Name)...); return err }
func (l winLauncher) Start() error     { _, err := runSchtasks(schtasksRunArgs(l.spec.Name)...); return err }
func (l winLauncher) Stop() error      { _, err := runSchtasks(schtasksEndArgs(l.spec.Name)...); return err }

func (l winLauncher) Status() (string, error) {
	out, err := runSchtasks(schtasksQueryArgs(l.spec.Name)...)
	if err != nil {
		// schtasks /Query exits non-zero when the task is not registered — the
		// common, expected case (e.g. setup deciding whether to install). We
		// collapse that to "unknown" rather than an error so callers can branch
		// on the string (install/skip), matching how the system Manager's status
		// is consumed. A rarer transient/permission error also maps here.
		return "unknown", nil
	}
	return parseSchtasksStatus(out), nil
}

func parseSchtasksStatus(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Status:") {
			v := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "Status:")))
			switch {
			case strings.Contains(v, "running"):
				return "running"
			case v != "":
				return "stopped"
			}
		}
	}
	return "unknown"
}

func newOSLauncher(spec LaunchSpec) (Launcher, error) { return winLauncher{spec: spec}, nil }
