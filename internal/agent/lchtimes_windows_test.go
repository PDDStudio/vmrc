package agent

import (
	"errors"
	"time"
)

// lchtimes is not implemented on Windows; symlink tests skip there.
func lchtimes(string, time.Time) error {
	return errors.New("setting a symlink's own mtime is not supported on windows")
}
