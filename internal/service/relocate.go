package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RelocateBinary copies srcExe into binDir/<name> (creating binDir) and returns
// the destination path. If src is already the destination, it is a no-op.
func RelocateBinary(srcExe, binDir, name string) (string, error) {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create bin dir %q: %w", binDir, err)
	}
	dst := filepath.Join(binDir, name)
	srcAbs, _ := filepath.Abs(srcExe)
	dstAbs, _ := filepath.Abs(dst)
	if srcAbs == dstAbs {
		return dst, nil
	}
	if err := copyFile(srcExe, dst, 0o755); err != nil {
		return "", err
	}
	return dst, nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %q: %w", src, err)
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("create %q: %w", tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("copy to %q: %w", tmp, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// CopyTree recursively copies srcDir into dstDir (files 0600, dirs 0700).
func CopyTree(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyFile(p, target, 0o600)
	})
}

// CopyFile copies src to dst (perm 0600), creating parent dirs.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return copyFile(src, dst, 0o600)
}
