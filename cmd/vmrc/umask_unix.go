//go:build !windows

package main

import (
	"os"
	"syscall"
)

// processUmask returns the current process umask without leaving it changed,
// so a fresh download can mimic os.Create's new-file permissions (0666 &^
// umask). Reading it briefly sets umask to 0; the CLI is single-threaded at
// download time, so no concurrent file creation observes the window.
func processUmask() os.FileMode {
	m := syscall.Umask(0)
	syscall.Umask(m)
	return os.FileMode(m)
}
