//go:build windows

package service

import "os"

// osElevated reports whether the process can open a privileged device, which
// only Administrators/SYSTEM can. This avoids pulling in golang.org/x/sys.
//
// Known limitation: on VMs that do not expose \\.\PHYSICALDRIVE0 (e.g. some
// Hyper-V/Azure configurations) this returns false even for an elevated
// process. Since the elevation check only gates a clearer error message before
// the privileged service operation, a false negative blocks a legitimately
// elevated user; revisit with a token-based check (golang.org/x/sys/windows,
// already an indirect dependency) if this proves to bite real VMs.
func osElevated() bool {
	f, err := os.Open(`\\.\PHYSICALDRIVE0`)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
