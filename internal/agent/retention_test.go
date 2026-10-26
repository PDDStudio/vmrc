package agent

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixed "now" for sweeper tests; file ages are set relative to it via Chtimes.
var sweepNow = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func mkFile(t *testing.T, root, rel string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	setAge(t, p, age)
	return p
}

func setAge(t *testing.T, p string, age time.Duration) {
	t.Helper()
	ts := sweepNow.Add(-age)
	if err := os.Chtimes(p, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func newSweeper(dir string, retention time.Duration) (*Sweeper, *bytes.Buffer) {
	var buf bytes.Buffer
	return &Sweeper{
		Dir:       dir,
		Retention: retention,
		Interval:  time.Hour,
		Now:       func() time.Time { return sweepNow },
		Logger:    log.New(&buf, "", 0),
	}, &buf
}

func TestSweepRemovesOnlyExpiredFiles(t *testing.T) {
	const ret = 7 * 24 * time.Hour
	tests := []struct {
		name     string
		rel      string
		age      time.Duration
		wantGone bool
	}{
		{"old top-level", "old.txt", ret + time.Hour, true},
		{"fresh top-level", "new.txt", time.Hour, false},
		{"just under retention", "edge.txt", ret - time.Minute, false},
		{"old nested", "a/b/old.bin", ret * 2, true},
		{"fresh nested", "a/fresh.bin", time.Minute, false},
	}
	dir := t.TempDir()
	paths := map[string]string{}
	for _, tt := range tests {
		paths[tt.name] = mkFile(t, dir, tt.rel, tt.age)
	}
	s, buf := newSweeper(dir, ret)
	res, err := s.Sweep()
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gone := !exists(paths[tt.name]); gone != tt.wantGone {
				t.Fatalf("%s gone=%v, want %v", tt.rel, gone, tt.wantGone)
			}
		})
	}
	if res.Files != 2 {
		t.Errorf("Files removed = %d, want 2", res.Files)
	}
	if !strings.Contains(buf.String(), "removed 2 file(s)") {
		t.Errorf("sweep summary not logged; got:\n%s", buf.String())
	}
}

func TestSweepPrunesEmptiedDirsButNotRoot(t *testing.T) {
	const ret = time.Hour
	dir := t.TempDir()
	mkFile(t, dir, "a/b/c/old.txt", 2*ret)        // whole chain a/b/c becomes empty
	mkFile(t, dir, "keep/fresh.txt", time.Minute) // keep/ stays (non-empty)
	mkFile(t, dir, "mixed/old.txt", 2*ret)        // mixed/ keeps sub/fresh
	mkFile(t, dir, "mixed/sub/fresh.txt", time.Minute)

	// A pre-existing, long-untouched empty dir is pruned too…
	staleEmpty := filepath.Join(dir, "stale-empty")
	if err := os.Mkdir(staleEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	setAge(t, staleEmpty, 2*ret)
	// …but a freshly created empty dir (e.g. a /put in progress) is left alone.
	freshEmpty := filepath.Join(dir, "fresh-empty")
	if err := os.Mkdir(freshEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	setAge(t, freshEmpty, time.Second)
	// Directory mtimes must be set after their contents were created.
	for _, d := range []string{"a/b/c", "a/b", "a", "mixed"} {
		setAge(t, filepath.Join(dir, filepath.FromSlash(d)), time.Second)
	}

	s, _ := newSweeper(dir, ret)
	res, err := s.Sweep()
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	for _, gone := range []string{"a", "stale-empty", "mixed/old.txt"} {
		if exists(filepath.Join(dir, filepath.FromSlash(gone))) {
			t.Errorf("%s should have been removed", gone)
		}
	}
	for _, kept := range []string{"keep/fresh.txt", "mixed/sub/fresh.txt", "fresh-empty"} {
		if !exists(filepath.Join(dir, filepath.FromSlash(kept))) {
			t.Errorf("%s should have been kept", kept)
		}
	}
	if !exists(dir) {
		t.Fatal("uploads root itself must never be removed")
	}
	if res.Dirs != 4 { // a/b/c, a/b, a, stale-empty
		t.Errorf("Dirs removed = %d, want 4", res.Dirs)
	}

	// An entirely emptied root survives too.
	dir2 := t.TempDir()
	mkFile(t, dir2, "only.txt", 2*ret)
	s2, _ := newSweeper(dir2, ret)
	if _, err := s2.Sweep(); err != nil {
		t.Fatal(err)
	}
	if !exists(dir2) {
		t.Fatal("emptied uploads root must not be removed")
	}
}

func TestSweepDoesNotFollowSymlinks(t *testing.T) {
	const ret = time.Hour
	dir := t.TempDir()
	outside := t.TempDir()
	victim := mkFile(t, outside, "victim.txt", 10*ret) // old, but outside uploads
	victimDir := mkFile(t, outside, "sub/deep.txt", 10*ret)

	// Old symlink to an outside dir: the link itself is removed, the target untouched.
	oldLink := filepath.Join(dir, "oldlink")
	if err := os.Symlink(outside, oldLink); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	// Fresh symlink to an outside file: kept (its own mtime is recent), target untouched.
	freshLink := filepath.Join(dir, "freshlink")
	if err := os.Symlink(victim, freshLink); err != nil {
		t.Fatal(err)
	}
	setLinkAge := func(p string, age time.Duration) {
		ts := sweepNow.Add(-age)
		if err := lchtimes(p, ts); err != nil {
			t.Skipf("cannot set symlink mtime here: %v", err)
		}
	}
	setLinkAge(oldLink, 10*ret)
	setLinkAge(freshLink, time.Minute)

	s, _ := newSweeper(dir, ret)
	if _, err := s.Sweep(); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if exists(oldLink) {
		t.Error("expired symlink itself should be removed")
	}
	if !exists(freshLink) {
		t.Error("fresh symlink should be kept")
	}
	if !exists(victim) || !exists(victimDir) {
		t.Fatal("sweeper followed a symlink and deleted files outside the uploads dir")
	}
}

func TestSweepDisabledWhenRetentionZero(t *testing.T) {
	dir := t.TempDir()
	p := mkFile(t, dir, "ancient.txt", 1000*time.Hour)
	s, _ := newSweeper(dir, 0)
	res, err := s.Sweep()
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.Files != 0 || !exists(p) {
		t.Fatalf("retention 0 must disable deletion (removed=%d, exists=%v)", res.Files, exists(p))
	}

	// Run returns immediately when disabled.
	done := make(chan struct{})
	go func() { s.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run with retention 0 should return immediately")
	}
}

func TestSweepMissingDirIsNotAnError(t *testing.T) {
	s, _ := newSweeper(filepath.Join(t.TempDir(), "nope"), time.Hour)
	if _, err := s.Sweep(); err != nil {
		t.Fatalf("missing uploads dir should be a no-op, got %v", err)
	}
}

func TestSweeperRunSweepsAtStartupAndStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	p := mkFile(t, dir, "old.txt", 2*time.Hour)
	s, _ := newSweeper(dir, time.Hour)
	s.Interval = 10 * time.Millisecond
	swept := make(chan struct{}, 16)
	s.afterSweep = func() { swept <- struct{}{} }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	// Startup sweep plus at least one ticked sweep.
	for i := 0; i < 2; i++ {
		select {
		case <-swept:
		case <-time.After(5 * time.Second):
			t.Fatalf("sweep %d did not happen", i+1)
		}
	}
	if exists(p) {
		t.Fatal("startup sweep did not remove the expired file")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop on context cancellation")
	}
}
