//go:build windows

package atomicfile

import "os"

// processUmask is 0 on Windows, which has no umask (file modes are cosmetic and
// os.Chmod only toggles the read-only bit).
func processUmask() os.FileMode { return 0 }
