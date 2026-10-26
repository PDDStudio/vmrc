//go:build !windows

package agent

import (
	"time"

	"golang.org/x/sys/unix"
)

// lchtimes sets the access and modification time of the symlink p itself (not
// its target).
func lchtimes(p string, ts time.Time) error {
	tv := unix.NsecToTimeval(ts.UnixNano())
	return unix.Lutimes(p, []unix.Timeval{tv, tv})
}
