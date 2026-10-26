package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/pddstudio/vmrc/internal/config"
)

// setupLogging tees the standard logger to both stderr and a size-rotated,
// append-mode log file at path, creating the parent directory if needed.
// Foreground runs keep printing to the console; service/boot runs — where
// stderr is attached to nothing — get a durable file to follow along with and
// to diagnose a start-then-immediately-stop crash loop. Timestamps (with
// microseconds) and a per-start banner make interleaved restart cycles
// readable.
//
// Logging starts before the config is resolved (so config errors are captured),
// so the file begins with the compiled-in rotation defaults; the caller applies
// the configured log_max_size_mb / log_max_backups via SetLimits afterwards.
//
// The file is intentionally left open for the lifetime of the process: the
// agent is long-running, and not closing it means a final log.Fatal still lands
// in the file (os.Exit skips deferred closes). The OS reclaims the descriptor on
// exit; file writes are unbuffered syscalls, so nothing is lost.
func setupLogging(path, banner string) (*rotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	rf, err := openRotatingFile(path, megabytes(config.DefaultLogMaxSizeMB), config.DefaultLogMaxBackups)
	if err != nil {
		return nil, err
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(os.Stderr, rf))
	log.Printf("=== %s ===", banner)
	return rf, nil
}

// megabytes converts a log_max_size_mb value to bytes.
func megabytes(mb int) int64 { return int64(mb) << 20 }

// rotatingFile is an append-mode log file that rotates by size: when a write
// would push it past maxBytes, agent.log is renamed to agent.log.1 (shifting
// .1→.2 … and dropping the oldest beyond maxBackups) and a fresh file is opened.
// It is mutex-guarded so concurrent writers never interleave with a rotation.
type rotatingFile struct {
	mu         sync.Mutex
	path       string
	f          *os.File
	size       int64
	maxBytes   int64 // 0 disables rotation
	maxBackups int   // rotated files kept; 0 means rotation just starts afresh
}

// openRotatingFile opens (or creates) path for appending, counting any existing
// content towards the size limit.
func openRotatingFile(path string, maxBytes int64, maxBackups int) (*rotatingFile, error) {
	rf := &rotatingFile{path: path, maxBytes: maxBytes, maxBackups: maxBackups}
	if err := rf.open(); err != nil {
		return nil, err
	}
	return rf, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", r.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat log file %q: %w", r.path, err)
	}
	r.f, r.size = f, info.Size()
	return nil
}

// SetLimits changes the rotation thresholds; maxBytes <= 0 disables rotation.
func (r *rotatingFile) SetLimits(maxBytes int64, maxBackups int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxBytes, r.maxBackups = maxBytes, maxBackups
}

// Write appends p, rotating first if p would push a non-empty file past the
// size limit. A single write larger than the limit still goes out whole.
func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.maxBytes > 0 && r.size > 0 && r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			// Keep logging rather than lose lines: report on stderr and carry on
			// with whatever file is open (rotate reopens on a best-effort basis).
			fmt.Fprintf(os.Stderr, "log rotation failed: %v\n", err)
		}
	}
	if r.f == nil {
		return 0, errors.New("log file unavailable")
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate shifts the backups and starts a fresh live file. The live file is
// closed before renaming because Windows cannot rename an open file. Must be
// called with r.mu held.
func (r *rotatingFile) rotate() error {
	if r.f != nil {
		_ = r.f.Close()
		r.f = nil
	}
	backup := func(i int) string { return fmt.Sprintf("%s.%d", r.path, i) }
	var firstErr error
	keep := func(err error) {
		if err != nil && !errors.Is(err, fs.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	if r.maxBackups > 0 {
		// Drop the oldest, then shift .i → .i+1 (removing the target first: on
		// Windows a rename onto an existing file fails).
		keep(os.Remove(backup(r.maxBackups)))
		for i := r.maxBackups - 1; i >= 1; i-- {
			keep(os.Rename(backup(i), backup(i+1)))
		}
		keep(os.Rename(r.path, backup(1)))
	} else {
		keep(os.Remove(r.path))
	}
	if err := r.open(); err != nil {
		return err
	}
	return firstErr
}

// Close closes the underlying file.
func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
