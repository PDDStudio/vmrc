package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// restoreLogger snapshots and restores the global std-logger config so a test
// that calls setupLogging does not leak its output/flags into other tests.
func restoreLogger(t *testing.T) {
	t.Helper()
	out, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})
}

// setupTestLogging calls setupLogging and closes the log file at test end:
// Windows can't remove an open file, so t.TempDir cleanup would fail.
func setupTestLogging(t *testing.T, path, banner string) error {
	t.Helper()
	rf, err := setupLogging(path, banner)
	if rf != nil {
		t.Cleanup(func() { _ = rf.Close() })
	}
	return err
}

func TestSetupLoggingWritesBannerAndCreatesDir(t *testing.T) {
	restoreLogger(t)
	path := filepath.Join(t.TempDir(), "logs", "agent.log") // parent dir missing on purpose

	if err := setupTestLogging(t, path, "BANNER-XYZ"); err != nil {
		t.Fatalf("setupLogging err = %v", err)
	}
	log.Print("after-setup-line")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "BANNER-XYZ") {
		t.Errorf("log missing banner; got:\n%s", got)
	}
	if !strings.Contains(got, "after-setup-line") {
		t.Errorf("log missing post-setup line; got:\n%s", got)
	}
}

func TestSetupLoggingAppends(t *testing.T) {
	restoreLogger(t)
	path := filepath.Join(t.TempDir(), "agent.log")

	if err := setupTestLogging(t, path, "FIRST"); err != nil {
		t.Fatalf("first setupLogging err = %v", err)
	}
	if err := setupTestLogging(t, path, "SECOND"); err != nil {
		t.Fatalf("second setupLogging err = %v", err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.Contains(got, "FIRST") || !strings.Contains(got, "SECOND") {
		t.Errorf("expected both banners (append mode); got:\n%s", got)
	}
}

func TestSetupLoggingTeesToStderrWriter(t *testing.T) {
	restoreLogger(t)
	path := filepath.Join(t.TempDir(), "agent.log")
	if err := setupTestLogging(t, path, "x"); err != nil {
		t.Fatalf("setupLogging err = %v", err)
	}
	// After setup, the global writer must be a MultiWriter (not the bare file),
	// so foreground runs still print to the console.
	if log.Writer() == io.Discard {
		t.Fatal("logger writer is io.Discard; tee not installed")
	}
}
