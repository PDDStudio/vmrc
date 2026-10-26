package config

import (
	"path/filepath"
	"runtime"
)

// testAbs turns a slash-separated absolute test path into an absolute path on
// the host OS (on Windows it needs a volume: "/abs/state" -> `C:\abs\state`).
func testAbs(p string) string {
	if runtime.GOOS == "windows" {
		return `C:` + filepath.FromSlash(p)
	}
	return p
}
