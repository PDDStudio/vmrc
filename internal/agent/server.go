// Package agent serves on-demand screenshots, file reads, a command runner, a
// detached launcher, and file transfer over a private (Tailscale-only) HTTP
// listener, gated by a token. /exec + /spawn + /get + /put turn the VM into a
// controllable build host (run tooling, start/stop processes, pull build
// outputs) without relying on RDP/SSH. Every request is logged to the console
// so the operator watching the VM can see what the remote driver is doing.
package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pddstudio/vmrc/internal/selfupdate"
)

// execTimeout bounds a single /exec command (packing/builds can be slow).
const execTimeout = 10 * time.Minute

// ErrNoInteractiveSession is returned by a Capturer when no user is logged into
// the interactive (console) session, so the desktop cannot be captured. The
// screenshot handler maps it to HTTP 409 rather than a generic 500.
var ErrNoInteractiveSession = errors.New("no interactive session")

// Capturer returns a PNG snapshot of the desktop. The real implementation is in
// capture.go (platform build tags); tests inject a fake.
type Capturer interface {
	Capture() ([]byte, error)
}

// Server holds the token, the screen capturer, the base directory that bounds
// /exec+/spawn (cwd), /get and /read (file access), and an optional Logger
// (defaults to the standard logger, which the agent points at the console).
type Server struct {
	Token    string
	Capturer Capturer
	Injector Injector
	LogDir   string
	BaseDir  string // root for /exec, /spawn (cwd), /get and /read; defaults to LogDir
	// UploadsDir is the root /put writes into (the agent passes
	// <base_dir>/uploads by default). Empty falls back to the base dir.
	UploadsDir string
	ReadGlobs  []string // allowlist of path globs readable via /read/ (and /logs/ alias)
	Logger     *log.Logger
	// Authorizer gates every request. If nil, a TokenAuthorizer derived from
	// Token is used (P3 will inject an identity-based one here).
	Authorizer Authorizer
	// Update enables the /update endpoint (remote self-update). When Update.Repo
	// is empty, /update returns 501 Not Implemented.
	Update UpdateConfig
	// ExitFunc terminates the process after a successful update so the service
	// supervisor relaunches the new binary; defaults to os.Exit. Injected in
	// tests. RestartDelay bounds the flush window before that exit (default
	// restartDelayDefault).
	ExitFunc     func(int)
	RestartDelay time.Duration
	// execPathFn overrides os.Executable in tests so a POST /update applies to a
	// temp file instead of the test binary. nil uses os.Executable.
	execPathFn func() (string, error)
	// spawnReaped, if non-nil, is called (from the reaper goroutine) after a
	// /spawn child has been waited on and reaped. Test-only observability seam;
	// nil in production.
	spawnReaped func(pid int)
}

// UpdateConfig describes the running binary so the /update handler can pick the
// matching GitHub release asset and replace it.
type UpdateConfig struct {
	Repo        string // "pddstudio/vmrc"
	BinaryName  string // "vmrc-agent"
	Version     string // current build version
	GitHubToken string // optional; raises the GitHub API rate limit
	APIBase     string // optional GitHub API root override (tests/proxies)

	// trustedKeys overrides the release signing keys embedded in the binary.
	// Tests only (unexported, never configurable): production always verifies
	// against the embedded keys.
	trustedKeys []ed25519.PublicKey
}

// restartDelayDefault is the flush window between writing the /update response
// and exiting to let the supervisor relaunch the new binary.
const restartDelayDefault = 750 * time.Millisecond

func (s *Server) exit() func(int) {
	if s.ExitFunc != nil {
		return s.ExitFunc
	}
	return os.Exit
}

func (s *Server) restartDelay() time.Duration {
	if s.RestartDelay > 0 {
		return s.RestartDelay
	}
	return restartDelayDefault
}

func (s *Server) executable() (string, error) {
	if s.execPathFn != nil {
		return s.execPathFn()
	}
	return os.Executable()
}

// Handler returns the HTTP mux for all endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/screenshot", s.handleScreenshot)
	mux.HandleFunc("/click", s.handleClick)
	mux.HandleFunc("/type", s.handleType)
	mux.HandleFunc("/key", s.handleKey)
	mux.HandleFunc("/read/", s.handleRead)
	mux.HandleFunc("/logs/", s.handleRead) // backward-compatible alias
	mux.HandleFunc("/exec", s.handleExec)
	mux.HandleFunc("/spawn", s.handleSpawn)
	mux.HandleFunc("/get", s.handleGet)
	mux.HandleFunc("/put", s.handlePut)
	mux.HandleFunc("/update", s.handleUpdate)
	return mux
}

// safeJoin resolves rel against the base dir, refusing to escape it.
func (s *Server) safeJoin(rel string) (string, bool) {
	return safeJoinUnder(s.base(), rel)
}

// safeJoinUnder resolves rel against root, refusing to escape it. It rejects
// both lexical traversal (../) and symlink escape: a symlink inside root that
// points outside would pass a plain prefix check, so the resolved paths are
// compared as well.
func safeJoinUnder(root, rel string) (string, bool) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	full, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil || (full != base && !strings.HasPrefix(full, base+string(os.PathSeparator))) {
		return "", false
	}
	// Symlink-aware containment: resolve symlinks on both base and the deepest
	// existing ancestor of full, then re-check. base itself is resolved because
	// temp/app roots can be symlinks (e.g. macOS /var -> /private/var). full may
	// not exist yet (a /put target), so only its existing prefix is resolved.
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", false
	}
	resolvedFull, err := resolveExisting(full)
	if err != nil {
		return "", false
	}
	if resolvedFull != resolvedBase && !strings.HasPrefix(resolvedFull, resolvedBase+string(os.PathSeparator)) {
		return "", false
	}
	return full, true
}

// resolveExisting resolves symlinks in p. When p does not exist, it resolves the
// deepest existing ancestor and rejoins the remaining (not-yet-created) suffix,
// so a target whose parent chain contains a symlink is still checked.
func resolveExisting(p string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(p)
	if parent == p {
		return "", fmt.Errorf("cannot resolve path %q", p)
	}
	resolvedParent, err := resolveExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(p)), nil
}

// logf writes a status line to the console (or the injected logger in tests).
func (s *Server) logf(format string, a ...any) {
	l := s.Logger
	if l == nil {
		l = log.Default()
	}
	l.Printf(format, a...)
}

// base returns the directory that bounds /exec, /spawn, /get and /read; falls
// back to LogDir.
func (s *Server) base() string {
	if s.BaseDir != "" {
		return s.BaseDir
	}
	return s.LogDir
}

// uploads returns the directory that bounds /put; falls back to the base dir.
func (s *Server) uploads() string {
	if s.UploadsDir != "" {
		return s.UploadsDir
	}
	return s.base()
}

// authorizer returns the configured Authorizer, or a token-based default
// derived from Token.
func (s *Server) authorizer() Authorizer {
	if s.Authorizer != nil {
		return s.Authorizer
	}
	return TokenAuthorizer{Token: s.Token}
}

func (s *Server) authed(w http.ResponseWriter, r *http.Request) bool {
	if err := s.authorizer().Authorize(r); err != nil {
		s.logf("DENIED  %s %s (%v)", r.Method, r.URL.Path, err)
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	png, err := s.Capturer.Capture()
	if err != nil {
		s.logf("screenshot ERROR: %v", err)
		if errors.Is(err, ErrNoInteractiveSession) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.logf("screenshot -> %d bytes", len(png))
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png) // best-effort; client may have disconnected
}

// injectErr maps an Injector error to an HTTP response: a missing interactive
// session (e.g. a session-0 service, or no user logged in) is 409, anything
// else 500. Mirrors handleScreenshot's treatment of ErrNoInteractiveSession.
func (s *Server) injectErr(w http.ResponseWriter, verb string, err error) {
	s.logf("%s ERROR: %v", verb, err)
	if errors.Is(err, ErrNoInteractiveSession) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// handleClick injects a mouse click at absolute screen pixel (?x, ?y) with the
// given ?button (left|right|middle, default left); ?double=1 double-clicks.
func (s *Server) handleClick(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	q := r.URL.Query()
	x, err := strconv.Atoi(q.Get("x"))
	if err != nil {
		http.Error(w, "missing or invalid x", http.StatusBadRequest)
		return
	}
	y, err := strconv.Atoi(q.Get("y"))
	if err != nil {
		http.Error(w, "missing or invalid y", http.StatusBadRequest)
		return
	}
	button := q.Get("button")
	double, _ := strconv.ParseBool(q.Get("double"))
	s.logf("click  > (%d,%d) button=%q double=%t", x, y, button, double)
	if err := s.Injector.Click(x, y, button, double); err != nil {
		s.injectErr(w, "click", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "clicked (%d,%d)\n", x, y)
}

// handleType types the literal UTF-8 ?text as Unicode keystrokes.
func (s *Server) handleType(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	text := r.URL.Query().Get("text")
	if text == "" {
		http.Error(w, "missing text", http.StatusBadRequest)
		return
	}
	s.logf("type   > %d chars", len([]rune(text)))
	if err := s.Injector.Type(text); err != nil {
		s.injectErr(w, "type", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "typed %d chars\n", len([]rune(text)))
}

// handleKey presses the ?combo key combination (e.g. "Enter", "Ctrl+C").
func (s *Server) handleKey(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	combo := r.URL.Query().Get("combo")
	if combo == "" {
		http.Error(w, "missing combo", http.StatusBadRequest)
		return
	}
	s.logf("key    > %s", combo)
	if err := s.Injector.Key(combo); err != nil {
		s.injectErr(w, "key", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "pressed %s\n", combo)
}

// readMatches reports whether rel matches any configured read glob. With no
// globs configured, nothing is readable (deny by default).
func (s *Server) readMatches(rel string) bool {
	for _, g := range s.ReadGlobs {
		if ok, _ := filepath.Match(g, rel); ok {
			return true
		}
	}
	return false
}

// handleRead serves a file under base_dir whose relative path matches a
// configured read glob. Served at /read/ (and the legacy /logs/ alias).
func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	// Strip whichever prefix matched (/read/ or /logs/).
	rel := strings.TrimPrefix(r.URL.Path, "/read/")
	rel = strings.TrimPrefix(rel, "/logs/")
	full, ok := s.safeJoin(rel)
	if !ok {
		s.logf("read   DENIED %q (outside base)", rel)
		http.Error(w, "forbidden path", http.StatusForbidden)
		return
	}
	if !s.readMatches(rel) {
		s.logf("read   DENIED %q (no matching glob)", rel)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		s.logf("read   DENIED %q (missing or directory)", rel)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.logf("read   -> %s", rel)
	http.ServeFile(w, r, full)
}

// shellCommand wraps a command line in the platform shell.
func shellCommand(ctx context.Context, cmdline string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/c", cmdline)
	}
	return exec.CommandContext(ctx, "sh", "-c", cmdline)
}

// setWorkdir points the command at the base dir if it's a real directory.
func (s *Server) setWorkdir(c *exec.Cmd) {
	if dir := s.base(); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			c.Dir = dir
		}
	}
}

// handleExec runs ?cmd=... in the base dir and returns combined stdout+stderr.
// The exit status is reported in the X-Exit-Status header (0 = success).
func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	cmdline := r.URL.Query().Get("cmd")
	if cmdline == "" {
		http.Error(w, "missing cmd", http.StatusBadRequest)
		return
	}
	s.logf("exec   > %s", cmdline)
	ctx, cancel := context.WithTimeout(r.Context(), execTimeout)
	defer cancel()
	c := shellCommand(ctx, cmdline)
	s.setWorkdir(c)
	start := time.Now()
	out, err := c.CombinedOutput()
	status := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			status = ee.ExitCode()
		} else {
			status = -1 // failed to start / timed out
			out = append(out, []byte("\n[exec error] "+err.Error())...)
		}
	}
	s.logf("exec   < exit=%d (%s, %d bytes): %s", status, time.Since(start).Round(time.Millisecond), len(out), cmdline)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Exit-Status", strconv.Itoa(status))
	_, _ = w.Write(out)
}

// handleSpawn launches ?cmd=... detached in the base dir (fire-and-forget) and
// returns its PID immediately. Use it for GUI apps / long-running processes that
// must keep running after the request returns (unlike /exec, which waits).
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	cmdline := r.URL.Query().Get("cmd")
	if cmdline == "" {
		http.Error(w, "missing cmd", http.StatusBadRequest)
		return
	}
	c := shellCommand(context.Background(), cmdline)
	s.setWorkdir(c)
	// Detached: do not wire stdio to the response (so the request returns and the
	// child keeps running independently).
	if err := c.Start(); err != nil {
		s.logf("spawn  ERROR: %v: %s", err, cmdline)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pid := c.Process.Pid
	// Reap the child in the background so it does not linger as a zombie: on Unix
	// a detached child whose parent never waits stays defunct until the parent
	// exits. Wait does not affect the child's execution — it only collects the
	// exit status once the child finishes — so the process still runs
	// independently after this request returns. Replaces Process.Release(), which
	// dropped the handle without ever reaping.
	go func() {
		_ = c.Wait()
		if s.spawnReaped != nil {
			s.spawnReaped(pid)
		}
	}()
	s.logf("spawn  pid=%d (detached): %s", pid, cmdline)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "spawned pid %d\n", pid)
}

// handleGet serves ?path=<relative> from within the base dir (no escaping it).
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	full, ok := s.safeJoin(rel)
	if !ok {
		s.logf("get    DENIED %q (outside base)", rel)
		http.Error(w, "forbidden path", http.StatusForbidden)
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		s.logf("get    DENIED %q (missing or directory)", rel)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.logf("get    -> %s (%d bytes)", rel, info.Size())
	http.ServeFile(w, r, full)
}

// handlePut writes the request body to ?path=<relative> within the uploads dir
// (UploadsDir; falls back to the base dir), creating parent directories. With
// the default layout the upload is then visible to /get, /exec and /spawn as
// uploads/<path> relative to the base dir. Upload counterpart to /get. POST/PUT
// only.
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "use POST or PUT", http.StatusMethodNotAllowed)
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	root := s.uploads()
	// Self-heal a removed uploads dir: safeJoinUnder resolves the root, so it
	// must exist before the containment check.
	if s.UploadsDir != "" {
		if err := os.MkdirAll(root, 0o750); err != nil {
			s.logf("put    ERROR creating uploads dir %s: %v", root, err)
			http.Error(w, "uploads dir unavailable", http.StatusInternalServerError)
			return
		}
	}
	full, ok := safeJoinUnder(root, rel)
	if !ok {
		s.logf("put    DENIED %q (outside uploads dir)", rel)
		http.Error(w, "forbidden path", http.StatusForbidden)
		return
	}
	f, err := createWithParents(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	n, err := io.Copy(f, r.Body)
	cerr := f.Close()
	if err != nil || cerr != nil {
		s.logf("put    ERROR %s: %v %v", rel, err, cerr)
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	s.logf("put    <- %s (%d bytes)", rel, n)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "wrote %d bytes to %s\n", n, rel)
}

// createWithParents creates (truncating) the file at full, making its parent
// directories first. If the parent vanishes between MkdirAll and Create — the
// retention sweeper may prune a directory it just emptied — it retries once.
func createWithParents(full string) (*os.File, error) {
	var f *os.File
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if err = os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		f, err = os.Create(full)
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return f, err
		}
	}
	return nil, err
}

// updateStatus is the JSON reply to GET /update?check=1.
type updateStatus struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Available  bool   `json:"available"`
	Prerelease bool   `json:"prerelease"`
	Notes      string `json:"notes,omitempty"`
}

// updateResult is the JSON reply to a POST /update.
type updateResult struct {
	Old        string `json:"old"`
	New        string `json:"new"`
	Restarting bool   `json:"restarting"`
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handleUpdate checks GitHub for a newer agent release (GET ?check=1) or applies
// one and restarts the process (POST). Replacing the binary while the service
// runs is safe — the underlying replace renames the running image aside — but to
// actually run the new binary the process must exit so its service supervisor
// relaunches it (systemd Restart=always / launchd KeepAlive / Windows
// OnFailure:restart all relaunch on a non-zero exit). The exit is therefore
// non-zero and deferred briefly so the HTTP response flushes first.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	if s.Update.Repo == "" {
		http.Error(w, "self-update not configured", http.StatusNotImplemented)
		return
	}
	q := r.URL.Query()
	t := selfupdate.Target{
		Repo:        s.Update.Repo,
		BinaryName:  s.Update.BinaryName,
		Version:     s.Update.Version,
		Token:       s.Update.GitHubToken,
		APIBase:     s.Update.APIBase,
		TrustedKeys: s.Update.trustedKeys,
	}
	opt := selfupdate.Options{PinnedTag: q.Get("version"), AllowPrerelease: q.Get("pre") != ""}

	rel, err := selfupdate.Check(r.Context(), t, opt)
	upToDate := errors.Is(err, selfupdate.ErrUpToDate)
	if err != nil && !upToDate {
		s.logf("update DENIED/ERROR check: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// GET ?check=1: report only.
	if r.Method == http.MethodGet {
		st := updateStatus{Current: s.Update.Version, Latest: s.Update.Version}
		if !upToDate {
			st.Latest, st.Available, st.Prerelease, st.Notes = rel.TagName, true, rel.Prerelease, rel.Notes
		}
		s.logf("update check: current=%s latest=%s available=%v", st.Current, st.Latest, st.Available)
		s.writeJSON(w, st)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "use GET ?check=1 or POST", http.StatusMethodNotAllowed)
		return
	}

	// POST: apply.
	if upToDate {
		s.writeJSON(w, updateResult{Old: s.Update.Version, New: s.Update.Version, Restarting: false})
		return
	}
	if err := selfupdate.GateDevBuild(s.Update.Version, q.Get("force") != "", q.Get("version")); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	exe, err := s.executable()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.logf("update applying %s -> %s at %s", s.Update.Version, rel.TagName, exe)
	if err := selfupdate.Apply(r.Context(), t, rel, exe); err != nil {
		s.logf("update ERROR apply: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.logf("update applied -> %s; restarting", rel.TagName)
	s.writeJSON(w, updateResult{Old: s.Update.Version, New: rel.TagName, Restarting: true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	// Exit non-zero after a brief flush window so the supervisor relaunches the
	// new binary (resolved via os.Executable() at launch).
	go func(exit func(int), delay time.Duration) {
		time.Sleep(delay)
		exit(1)
	}(s.exit(), s.restartDelay())
}
