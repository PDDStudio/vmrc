// Package atomicfile writes downloaded content to a local path atomically, so a
// failed transfer never truncates or corrupts a pre-existing file. It is shared
// by the vmrc CLI (`vmrc get`) and the MCP server (`vmrc_get` with local_path).
package atomicfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Write streams src into local atomically: it writes to a temp file in the same
// directory and renames it over local only after a fully successful copy and
// flush. A mid-transfer failure (dropped connection, disk-full on flush)
// therefore never truncates or leaves behind a partial local file — the
// pre-existing file, if any, is untouched. It returns the number of bytes
// written.
func Write(local string, src io.Reader) (int64, error) {
	dir := filepath.Dir(local)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(local)+".vmrc-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	n, err := io.Copy(tmp, src)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return n, err
	}
	// Flush/close before rename: a failed Close (flush/disk-full) means a
	// truncated temp file, so surface it and discard the temp rather than
	// promoting a corrupt file over the destination.
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return n, fmt.Errorf("close %s: %w", tmpName, err)
	}
	// Fix the temp file's mode before promoting it: CreateTemp makes it 0600, but
	// replacing an existing file must keep that file's mode (e.g. an executable
	// stays +x) and a new file should follow os.Create semantics (0666 & ~umask).
	mode := os.FileMode(0o666) &^ processUmask()
	if fi, err := os.Stat(local); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		_ = os.Remove(tmpName)
		return n, fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, local); err != nil {
		_ = os.Remove(tmpName)
		return n, fmt.Errorf("rename to %s: %w", local, err)
	}
	return n, nil
}
