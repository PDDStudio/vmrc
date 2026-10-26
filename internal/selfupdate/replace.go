package selfupdate

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/minio/selfupdate"
)

// applyBinary atomically replaces the executable at targetPath with newExe. It
// delegates to minio/selfupdate, which performs the cross-platform rename-aside
// swap (so even a running Windows .exe, which cannot be overwritten in place,
// can be replaced) and rolls the old binary back if the swap fails.
//
// The staging file minio writes (<dir>/.<name>.new) is opened with O_TRUNC, not
// O_EXCL, and renamed into place without being re-read. So that a file planted
// at that path by someone else (who could keep a handle open and rewrite it
// between write and rename) can never become the new binary:
//   - any pre-existing staging file is removed first, and the update is refused
//     if it cannot be removed;
//   - the SHA-256 of the verified, extracted binary is passed as
//     Options.Checksum, and the staged file is re-hashed from disk before the
//     rename is committed.
func applyBinary(newExe []byte, targetPath string) error {
	sum := sha256.Sum256(newExe)
	opts := selfupdate.Options{TargetPath: targetPath, Checksum: sum[:]}
	staged := stagingPath(targetPath)

	if err := removeStaleStaging(staged); err != nil {
		return err
	}
	if err := selfupdate.PrepareAndCheckBinary(bytes.NewReader(newExe), opts); err != nil {
		return err
	}
	if err := verifyFileSHA256(staged, sum[:]); err != nil {
		_ = os.Remove(staged)
		return err
	}
	return selfupdate.CommitBinary(opts)
}

// stagingPath mirrors minio/selfupdate's staging-file name for targetPath.
func stagingPath(targetPath string) string {
	return filepath.Join(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".new")
}

// removeStaleStaging deletes a leftover staging file (from a crashed update or
// planted by another user). A missing file is fine; one that cannot be removed
// means we cannot guarantee the staged bytes are ours, so the update is refused.
func removeStaleStaging(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("refusing to update: stale staging file %s exists and cannot be removed: %w", path, err)
}

// verifyFileSHA256 re-hashes the file at path and compares it to want.
func verifyFileSHA256(path string, want []byte) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("re-reading staged binary: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("re-reading staged binary: %w", err)
	}
	if got := h.Sum(nil); !bytes.Equal(got, want) {
		return fmt.Errorf("%w: staged binary %s changed on disk (want %x, got %x)", ErrChecksum, path, want, got)
	}
	return nil
}
