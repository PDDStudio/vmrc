//go:build !windows

package atomicfile

import (
	"os"
	"sync"
	"syscall"
)

var (
	umaskOnce sync.Once
	umaskVal  os.FileMode
)

// processUmask returns the process umask without leaving it changed, so a fresh
// download can mimic os.Create's new-file permissions (0666 &^ umask). Reading
// it briefly sets umask to 0, so it is read exactly once (on first use) and
// cached: the MCP server may run several downloads concurrently, and repeated
// reads would open a window where another goroutine's file creation observes
// umask 0. vmrc never changes its umask, so the cached value stays correct.
func processUmask() os.FileMode {
	umaskOnce.Do(func() {
		m := syscall.Umask(0)
		syscall.Umask(m)
		umaskVal = os.FileMode(m)
	})
	return umaskVal
}
