//go:build !windows

package config

// SecureDir is a no-op outside Windows: Unix installs rely on mode bits
// (0700 state dir, 0600 log) and root-owned system paths. See acl_windows.go.
func SecureDir(dir, readerSID string, noDescend []string) error { return nil }

// CurrentUserSID is Windows-only; elsewhere it returns "".
func CurrentUserSID() (string, error) { return "", nil }
