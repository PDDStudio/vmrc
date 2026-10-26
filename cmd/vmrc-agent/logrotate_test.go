package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func readOr(t *testing.T, p string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func TestRotatingFileRotatesAtSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	rf, err := openRotatingFile(path, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rf.Close() })

	for _, line := range []string{"aaaaaaaa\n", "bbbbbbbb\n"} { // 9 bytes each; 2nd would exceed 10
		if _, err := rf.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	cur, _ := readOr(t, path)
	old, ok := readOr(t, path+".1")
	if !ok || old != "aaaaaaaa\n" {
		t.Fatalf("agent.log.1 = %q (exists=%v), want first line", old, ok)
	}
	if cur != "bbbbbbbb\n" {
		t.Fatalf("agent.log = %q, want second line", cur)
	}
}

func TestRotatingFileCapsBackups(t *testing.T) {
	tests := []struct {
		name       string
		maxBackups int
		writes     int
		wantFiles  []string // suffixes that must exist ("" = the live file)
		wantAbsent []string
	}{
		{name: "three backups", maxBackups: 3, writes: 6, wantFiles: []string{"", ".1", ".2", ".3"}, wantAbsent: []string{".4", ".5"}},
		{name: "one backup", maxBackups: 1, writes: 4, wantFiles: []string{"", ".1"}, wantAbsent: []string{".2"}},
		{name: "zero backups truncates", maxBackups: 0, writes: 3, wantFiles: []string{""}, wantAbsent: []string{".1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.log")
			rf, err := openRotatingFile(path, 5, tt.maxBackups)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = rf.Close() })
			for i := 0; i < tt.writes; i++ {
				if _, err := fmt.Fprintf(rf, "w%03d\n", i); err != nil { // 5 bytes: each write rotates
					t.Fatal(err)
				}
			}
			for _, sfx := range tt.wantFiles {
				if _, ok := readOr(t, path+sfx); !ok {
					t.Errorf("agent.log%s missing", sfx)
				}
			}
			for _, sfx := range tt.wantAbsent {
				if _, ok := readOr(t, path+sfx); ok {
					t.Errorf("agent.log%s should have been dropped", sfx)
				}
			}
			// Newest content is live; .1 holds the previous write.
			if cur, _ := readOr(t, path); cur != fmt.Sprintf("w%03d\n", tt.writes-1) {
				t.Errorf("live file = %q", cur)
			}
			if tt.maxBackups > 0 {
				if prev, _ := readOr(t, path+".1"); prev != fmt.Sprintf("w%03d\n", tt.writes-2) {
					t.Errorf("agent.log.1 = %q", prev)
				}
			}
		})
	}
}

func TestRotatingFileZeroSizeDisablesRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	rf, err := openRotatingFile(path, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rf.Close() })
	for i := 0; i < 100; i++ {
		_, _ = rf.Write([]byte("0123456789\n"))
	}
	if _, ok := readOr(t, path+".1"); ok {
		t.Fatal("rotation must be disabled when max size is 0")
	}
	if cur, _ := readOr(t, path); len(cur) != 1100 {
		t.Fatalf("live file len = %d, want 1100", len(cur))
	}
}

func TestRotatingFileCountsExistingSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(path, []byte("12345678\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rf, err := openRotatingFile(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rf.Close() })
	if _, err := rf.Write([]byte("next\n")); err != nil {
		t.Fatal(err)
	}
	if old, _ := readOr(t, path+".1"); old != "12345678\n" {
		t.Fatalf("pre-existing content should rotate out on reopen; .1 = %q", old)
	}
}

func TestRotatingFileSetLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	rf, err := openRotatingFile(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rf.Close() })
	_, _ = rf.Write([]byte("0123456789\n"))
	rf.SetLimits(5, 1)
	_, _ = rf.Write([]byte("x\n"))
	if old, _ := readOr(t, path+".1"); old != "0123456789\n" {
		t.Fatalf("SetLimits not applied; .1 = %q", old)
	}
}

func TestRotatingFileConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	rf, err := openRotatingFile(path, 200, 50)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rf.Close() })
	const goroutines, per = 8, 50
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				_, _ = fmt.Fprintf(rf, "g%d-line%03d\n", g, i)
			}
		}(g)
	}
	wg.Wait()

	// Every line must land intact in exactly one file (no torn writes, nothing
	// lost while rotating) — 400 lines * ~12 bytes ≈ 4.8 KB fits in 50 backups.
	var all strings.Builder
	matches, _ := filepath.Glob(path + "*")
	for _, m := range matches {
		b, _ := os.ReadFile(m)
		all.Write(b)
	}
	lines := strings.Split(strings.TrimSpace(all.String()), "\n")
	if len(lines) != goroutines*per {
		t.Fatalf("got %d lines across %d files, want %d", len(lines), len(matches), goroutines*per)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "g") || !strings.Contains(l, "-line") {
			t.Fatalf("torn line %q", l)
		}
	}
}

func TestSetupLoggingReturnsRotatingWriter(t *testing.T) {
	restoreLogger(t)
	path := filepath.Join(t.TempDir(), "agent.log")
	rf, err := setupLogging(path, "B")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rf.Close() })
	rf.SetLimits(1, 1) // force a rotation on the next line
	log.Print("rotated-line")
	if old, ok := readOr(t, path+".1"); !ok || !strings.Contains(old, "=== B ===") {
		t.Fatalf("banner should have rotated into .1; got %q", old)
	}
	if cur, _ := readOr(t, path); !strings.Contains(cur, "rotated-line") {
		t.Fatalf("live log missing new line; got %q", cur)
	}
}
