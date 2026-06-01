//go:build windows

package service

import "golang.org/x/sys/windows"

// osElevated reports whether the current process token is elevated — i.e. it
// holds full Administrator (or SYSTEM, the boot-service account) privileges.
// It queries the Windows token API directly via the process pseudo-token, which
// is reliable on every VM, unlike a privileged-device probe that assumes a
// particular disk (\\.\PHYSICALDRIVE0) is present.
func osElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
