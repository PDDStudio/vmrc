//go:build windows

package service

import (
	"fmt"
	"os/exec"
	"strings"
)

func schtasksCreateArgs(s LaunchSpec) []string {
	tr := fmt.Sprintf("%q %s", s.Executable, strings.Join(s.Args, " "))
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
