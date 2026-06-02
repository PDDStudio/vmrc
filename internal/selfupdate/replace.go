package selfupdate

import (
	"bytes"

	"github.com/minio/selfupdate"
)

// applyBinary atomically replaces the executable at targetPath with newExe. It
// delegates to minio/selfupdate, which performs the cross-platform rename-aside
// swap (so even a running Windows .exe, which cannot be overwritten in place,
// can be replaced) and rolls the old binary back if the swap fails.
func applyBinary(newExe []byte, targetPath string) error {
	return selfupdate.Apply(bytes.NewReader(newExe), selfupdate.Options{TargetPath: targetPath})
}
