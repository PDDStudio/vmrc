//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func schtasksRunArgs(name string) []string    { return []string{"/Run", "/TN", name} }
func schtasksEndArgs(name string) []string    { return []string{"/End", "/TN", name} }
func schtasksDeleteArgs(name string) []string { return []string{"/Delete", "/TN", name, "/F"} }
func schtasksQueryArgs(name string) []string  { return []string{"/Query", "/TN", name, "/FO", "LIST", "/V"} }

func runSchtasks(args ...string) (string, error) {
	out, err := exec.Command("schtasks.exe", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("schtasks %v: %w: %s", args, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type winLauncher struct{ spec LaunchSpec }

func (l winLauncher) Install() error {
	f, err := os.CreateTemp("", "vmrc-task-*.xml")
	if err != nil {
		return fmt.Errorf("creating task xml temp file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(utf16LEBOM(taskXML(l.spec))); err != nil {
		f.Close()
		return fmt.Errorf("writing task xml: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing task xml: %w", err)
	}
	// /RU binds the principal so a HighestAvailable/InteractiveToken task created
	// from XML does not prompt for credentials.
	_, err = runSchtasks("/Create", "/TN", l.spec.Name, "/XML", tmp, "/RU", l.spec.User, "/F")
	return err
}
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
