package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
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

func TestSetupLoggingWritesBannerAndCreatesDir(t *testing.T) {
	restoreLogger(t)
	path := filepath.Join(t.TempDir(), "logs", "agent.log") // parent dir missing on purpose

	if _, err := setupLogging(path, "BANNER-XYZ"); err != nil {
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

	if _, err := setupLogging(path, "FIRST"); err != nil {
		t.Fatalf("first setupLogging err = %v", err)
	}
	if _, err := setupLogging(path, "SECOND"); err != nil {
		t.Fatalf("second setupLogging err = %v", err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.Contains(got, "FIRST") || !strings.Contains(got, "SECOND") {
		t.Errorf("expected both banners (append mode); got:\n%s", got)
	}
}

func TestSetupLoggingIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are not used on Windows (the dir DACL applies)")
	}
	restoreLogger(t)
	dir := filepath.Join(t.TempDir(), "logs")
	path := filepath.Join(dir, "agent.log")
	if _, err := setupLogging(path, "x"); err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	if st, err := os.Stat(dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("log dir mode = %v (err %v), want 0700", st.Mode().Perm(), err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("log file mode = %v (err %v), want 0600", st.Mode().Perm(), err)
	}
}

func TestSetupLoggingTightensExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are not used on Windows (the dir DACL applies)")
	}
	restoreLogger(t)
	path := filepath.Join(t.TempDir(), "agent.log")
	// A log left world-readable by an older version.
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := setupLogging(path, "x"); err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("existing log file mode = %v, want 0600", st.Mode().Perm())
	}
}

func TestSetupLoggingTeesToStderrWriter(t *testing.T) {
	restoreLogger(t)
	path := filepath.Join(t.TempDir(), "agent.log")
	if _, err := setupLogging(path, "x"); err != nil {
		t.Fatalf("setupLogging err = %v", err)
	}
	// After setup, the global writer must be a MultiWriter (not the bare file),
	// so foreground runs still print to the console.
	if log.Writer() == io.Discard {
		t.Fatal("logger writer is io.Discard; tee not installed")
	}
}
