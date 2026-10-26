//go:build !windows

package agent

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecTimeoutKillsProcessTreeHoldingStdout(t *testing.T) {
	s, base := newTestServer(t)
	s.ExecTimeout = 500 * time.Millisecond
	pidFile := filepath.Join(base, "child.pid")
	// The backgrounded sleep inherits stdout; before the fix the handler waited
	// for it (an hour) instead of the timeout.
	cmd := "sleep 3600 & echo $! > " + pidFile + "; echo started; wait"
	start := time.Now()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/exec?token=secret&cmd="+url.QueryEscape(cmd), nil))
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("exec returned after %s; timeout not enforced", d)
	}
	if rec.Header().Get("X-Exit-Status") == "0" {
		t.Fatalf("a timed-out command must not report success")
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("background child %d survived the exec timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestExecOutputCapped(t *testing.T) {
	s, _ := newTestServer(t)
	s.MaxExecOutput = 1000
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/exec?token=secret&cmd="+url.QueryEscape("head -c 100000 /dev/zero"), nil))
	if n := rec.Body.Len(); n > 1000+200 {
		t.Fatalf("output not capped: %d bytes", n)
	}
	if rec.Header().Get("X-Output-Truncated") != "true" {
		t.Fatalf("truncation must be signalled, headers=%v", rec.Header())
	}
	if rec.Header().Get("X-Exit-Status") != "0" {
		t.Fatalf("the command itself succeeded; exit=%s", rec.Header().Get("X-Exit-Status"))
	}
}

func TestExecReturnsWhenShellExitsButChildHoldsStdout(t *testing.T) {
	s, _ := newTestServer(t)
	start := time.Now()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/exec?token=secret&cmd="+url.QueryEscape("sleep 30 & echo started"), nil))
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("exec waited %s for a backgrounded child", d)
	}
	if rec.Header().Get("X-Exit-Status") != "0" || !strings.Contains(rec.Body.String(), "started") {
		t.Fatalf("exit=%s body=%q", rec.Header().Get("X-Exit-Status"), rec.Body.String())
	}
}
