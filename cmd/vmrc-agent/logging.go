package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// setupLogging tees the standard logger to both stderr and an append-mode log
// file at path, creating the parent directory if needed. Foreground runs keep
// printing to the console; service/boot runs — where stderr is attached to
// nothing — get a durable file to follow along with and to diagnose a
// start-then-immediately-stop crash loop. Timestamps (with microseconds) and a
// per-start banner make interleaved restart cycles readable.
//
// The file is intentionally left open for the lifetime of the process: the
// agent is long-running, and not closing it means a final log.Fatal still lands
// in the file (os.Exit skips deferred closes). The OS reclaims the descriptor on
// exit; file writes are unbuffered syscalls, so nothing is lost.
func setupLogging(path, banner string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", path, err)
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("=== %s ===", banner)
	return nil
}
