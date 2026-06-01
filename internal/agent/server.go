// Package agent serves on-demand screenshots, file reads, a command runner, a
// detached launcher, and file transfer over a private (Tailscale-only) HTTP
// listener, gated by a token. /exec + /spawn + /get + /put turn the VM into a
// controllable build host (run tooling, start/stop processes, pull build
// outputs) without relying on RDP/SSH. Every request is logged to the console
// so the operator watching the VM can see what the remote driver is doing.
package agent

import (
	"context"
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
)

// execTimeout bounds a single /exec command (packing/builds can be slow).
const execTimeout = 10 * time.Minute

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
	LogDir   string
	BaseDir  string // root for /exec, /spawn (cwd), /get and /read; defaults to LogDir
	Logger   *log.Logger
	// Authorizer gates every request. If nil, a TokenAuthorizer derived from
	// Token is used (P3 will inject an identity-based one here).
	Authorizer Authorizer
}

// allowedLogs is the fixed set of log files we expose. (Generalized to globs in
// a later task.)
var allowedLogs = map[string]bool{"error.log": true, "app.log": true}

// Handler returns the HTTP mux for all endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/screenshot", s.handleScreenshot)
	mux.HandleFunc("/logs/", s.handleLogs)
	mux.HandleFunc("/exec", s.handleExec)
	mux.HandleFunc("/spawn", s.handleSpawn)
	mux.HandleFunc("/get", s.handleGet)
	mux.HandleFunc("/put", s.handlePut)
	return mux
}

// safeJoin resolves rel against the base dir, refusing to escape it.
func (s *Server) safeJoin(rel string) (string, bool) {
	base, err := filepath.Abs(s.base())
	if err != nil {
		return "", false
	}
	full, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil || (full != base && !strings.HasPrefix(full, base+string(os.PathSeparator))) {
		return "", false
	}
	return full, true
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.logf("screenshot -> %d bytes", len(png))
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png) // best-effort; client may have disconnected
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	name := filepath.Base(r.URL.Path) // strips any path segments
	if !allowedLogs[name] {
		s.logf("logs   DENIED %q (not allowlisted)", name)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.logf("logs   -> %s", name)
	http.ServeFile(w, r, filepath.Join(s.base(), name))
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
	_ = c.Process.Release()
	s.logf("spawn  pid=%d (detached): %s", pid, cmdline)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "spawned pid %d\n", pid)
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
	if info, err := os.Stat(full); err == nil {
		s.logf("get    -> %s (%d bytes)", rel, info.Size())
	} else {
		s.logf("get    -> %s (missing)", rel)
	}
	http.ServeFile(w, r, full)
}

// handlePut writes the request body to ?path=<relative> within the base dir,
// creating parent directories. Upload counterpart to /get. POST/PUT only.
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
	full, ok := s.safeJoin(rel)
	if !ok {
		s.logf("put    DENIED %q (outside base)", rel)
		http.Error(w, "forbidden path", http.StatusForbidden)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f, err := os.Create(full)
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
	fmt.Fprintf(w, "wrote %d bytes to %s\n", n, rel)
}
