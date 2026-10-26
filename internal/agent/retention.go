package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxNamedRemovals is how many removed paths a sweep summary lists by name;
// beyond that only the counts are logged, so a large purge stays one line.
const maxNamedRemovals = 5

// Sweeper deletes uploads older than Retention from Dir. Each sweep removes
// regular files and symlinks (judged by their own mtime; symlinks are never
// followed) and then prunes directories left empty — never Dir itself.
type Sweeper struct {
	Dir       string        // uploads root
	Retention time.Duration // maximum age by mtime; <= 0 disables the sweeper
	Interval  time.Duration // time between sweeps (one also runs at startup)
	Now       func() time.Time
	Logger    *log.Logger // defaults to the standard logger

	// afterSweep, if non-nil, is called after each sweep in Run. Test seam.
	afterSweep func()
}

// SweepResult counts what one sweep removed.
type SweepResult struct {
	Files  int // regular files and symlinks removed
	Dirs   int // empty directories pruned
	Errors int // entries that could not be inspected or removed
}

func (s *Sweeper) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Sweeper) logf(format string, a ...any) {
	l := s.Logger
	if l == nil {
		l = log.Default()
	}
	l.Printf(format, a...)
}

// Run sweeps once immediately and then every Interval until ctx is canceled.
// It returns at once when retention is disabled.
func (s *Sweeper) Run(ctx context.Context) {
	if s.Retention <= 0 {
		s.logf("upload retention disabled; uploads in %s are kept indefinitely", s.Dir)
		return
	}
	interval := s.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	s.logf("upload retention: removing uploads older than %s from %s (sweep every %s)", s.Retention, s.Dir, interval)
	sweep := func() {
		if _, err := s.Sweep(); err != nil {
			s.logf("retention sweep ERROR: %v", err)
		}
		if s.afterSweep != nil {
			s.afterSweep()
		}
	}
	sweep()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}

// Sweep performs one retention pass. A missing Dir is not an error (nothing has
// been uploaded yet). Per-entry failures are counted and summarized rather than
// aborting the pass.
func (s *Sweeper) Sweep() (SweepResult, error) {
	var res SweepResult
	if s.Retention <= 0 {
		return res, nil
	}
	// Resolve the root once so a symlinked uploads_dir (or a symlinked parent,
	// e.g. macOS /var) is honored; nothing below the root is ever followed.
	root, err := filepath.EvalSymlinks(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("resolve uploads dir %q: %w", s.Dir, err)
	}
	cutoff := s.now().Add(-s.Retention)

	type dirEntry struct {
		path  string
		stale bool
	}
	var (
		dirs     []dirEntry          // pre-order; walked in reverse for deepest-first pruning
		emptied  = map[string]bool{} // dirs we removed something from in this pass
		named    []string
		firstErr error
	)
	noteErr := func(err error) {
		res.Errors++
		if firstErr == nil {
			firstErr = err
		}
	}
	removed := func(p string) {
		emptied[filepath.Dir(p)] = true
		if len(named) < maxNamedRemovals+1 {
			if rel, err := filepath.Rel(root, p); err == nil {
				named = append(named, filepath.ToSlash(rel))
			}
		}
	}

	// WalkDir reports entries via Lstat and never descends into symlinks.
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			noteErr(err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == root {
			return nil
		}
		info, err := d.Info() // Lstat semantics: a symlink's own mtime
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				noteErr(err)
			}
			return nil
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			dirs = append(dirs, dirEntry{path: p, stale: info.ModTime().Before(cutoff)})
		case mode.IsRegular() || mode&fs.ModeSymlink != 0:
			if !info.ModTime().Before(cutoff) {
				return nil
			}
			if err := os.Remove(p); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					noteErr(err)
				}
				return nil
			}
			res.Files++
			removed(p)
		}
		return nil
	})
	if walkErr != nil {
		return res, fmt.Errorf("walk uploads dir %q: %w", root, walkErr)
	}

	// Deepest first, so a chain emptied in this pass collapses in one sweep. A
	// dir is pruned only if it is empty and either we emptied it just now or it
	// has itself been untouched for the whole retention period — a fresh empty
	// dir may be a /put in progress. os.Remove refuses non-empty dirs.
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		if !d.stale && !emptied[d.path] {
			continue
		}
		if err := os.Remove(d.path); err != nil {
			continue // not empty (or raced away): leave it
		}
		res.Dirs++
		removed(d.path)
	}

	if res.Files > 0 || res.Dirs > 0 {
		detail := ""
		if len(named) <= maxNamedRemovals {
			detail = ": " + strings.Join(named, ", ")
		}
		s.logf("retention sweep: removed %d file(s), %d empty dir(s) older than %s from %s%s",
			res.Files, res.Dirs, s.Retention, s.Dir, detail)
	}
	if res.Errors > 0 {
		s.logf("retention sweep: %d error(s); first: %v", res.Errors, firstErr)
	}
	return res, nil
}
