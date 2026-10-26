package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

// Log file permissions. The request log records every exec/spawn command line
// (which may carry credentials an operator typed), so it is owner-only. On
// Windows mode bits are ignored; the directory DACL (config.SecureDir) applies.
const (
	logDirPerm  os.FileMode = 0o700
	logFilePerm os.FileMode = 0o600
)

// setupLogging tees the standard logger to both stderr and an append-mode log
// file at path, creating the parent directory if needed. Foreground runs keep
// printing to the console; service/boot runs — where stderr is attached to
// nothing — get a durable file to follow along with and to diagnose a
// start-then-immediately-stop crash loop. Timestamps (with microseconds) and a
// per-start banner make interleaved restart cycles readable.
//
// A newly created log dir is 0700 and the file is 0600; an existing file left
// world-readable by an older version is tightened to 0600. An existing dir is
// left alone (an explicit --log-file may point into a shared dir like /var/log).
//
// The file is intentionally left open for the lifetime of the process: the
// agent is long-running, and not closing it means a final log.Fatal still lands
// in the file (os.Exit skips deferred closes). The OS reclaims the descriptor on
// exit; file writes are unbuffered syscalls, so nothing is lost.
func setupLogging(path, banner string) error {
	if err := os.MkdirAll(filepath.Dir(path), logDirPerm); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePerm)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", path, err)
	}
	if runtime.GOOS != "windows" {
		if err := f.Chmod(logFilePerm); err != nil {
			fmt.Fprintf(os.Stderr, "warning: tightening log file %q to %o: %v\n", path, logFilePerm, err)
		}
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("=== %s ===", banner)
	return nil
}
