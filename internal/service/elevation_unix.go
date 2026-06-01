//go:build !windows

package service

import "os"

// osElevated reports root on unix-like systems.
func osElevated() bool { return os.Geteuid() == 0 }
