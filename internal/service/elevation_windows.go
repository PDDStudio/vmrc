//go:build windows

package service

import "os"

// osElevated reports whether the process can open a privileged device, which
// only Administrators/SYSTEM can. This avoids pulling in golang.org/x/sys.
func osElevated() bool {
	f, err := os.Open(`\\.\PHYSICALDRIVE0`)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
