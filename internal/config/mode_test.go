package config

import (
	"runtime"
	"testing"
)

func TestUserPathsWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only path separators")
	}
	got := userPaths("windows", `C:\Users\dev\AppData\Local`)
	want := paths{
		ConfigPath: `C:\Users\dev\AppData\Local\vmrc\agent.json`,
		StateDir:   `C:\Users\dev\AppData\Local\vmrc\state`,
		BaseDir:    `C:\Users\dev\AppData\Local\vmrc\files`,
		BinDir:     `C:\Users\dev\AppData\Local\vmrc\bin`,
	}
	if got != want {
		t.Fatalf("userPaths windows:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestUserPathsUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only path separators")
	}
	got := userPaths("linux", "/home/dev/.local/state")
	want := paths{
		ConfigPath: "/home/dev/.local/state/vmrc/agent.json",
		StateDir:   "/home/dev/.local/state/vmrc/state",
		BaseDir:    "/home/dev/.local/state/vmrc/files",
		BinDir:     "/home/dev/.local/state/vmrc/bin",
	}
	if got != want {
		t.Fatalf("userPaths unix:\n got=%+v\nwant=%+v", got, want)
	}
}
