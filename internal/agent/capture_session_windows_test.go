//go:build windows

package agent

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// startProcess launches args and returns the started command plus a handle that
// awaitHelper can wait on / terminate. Hermetic: an ordinary child process, no
// session or token APIs.
func startProcess(t *testing.T, args ...string) (*exec.Cmd, windows.Handle) {
	t.Helper()
	c := exec.Command(args[0], args[1:]...)
	if err := c.Start(); err != nil {
		t.Fatalf("start %v: %v", args, err)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(c.Process.Pid))
	if err != nil {
		_ = c.Process.Kill()
		t.Fatalf("OpenProcess: %v", err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(h) })
	return c, h
}

func TestAwaitHelperKillsAStuckHelper(t *testing.T) {
	// ping directly (not via cmd) so terminating it leaves no orphan behind.
	c, h := startProcess(t, "ping.exe", "-n", "60", "127.0.0.1")
	never := make(chan pipeResult) // the output never arrives

	start := time.Now()
	_, err := awaitHelper(h, never, 300*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("awaitHelper overran its timeout: %s", time.Since(start))
	}
	waited := make(chan struct{})
	go func() { _ = c.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("helper process was not terminated")
	}
}

func TestAwaitHelperReturnsOutputOnSuccess(t *testing.T) {
	c, h := startProcess(t, "cmd.exe", "/c", "exit 0")
	results := make(chan pipeResult, 1)
	results <- pipeResult{data: []byte("PNG")}
	got, err := awaitHelper(h, results, 10*time.Second)
	if err != nil || string(got) != "PNG" {
		t.Fatalf("got %q, %v; want PNG, nil", got, err)
	}
	_ = c.Wait()
}

func TestAwaitHelperReportsNonZeroExit(t *testing.T) {
	c, h := startProcess(t, "cmd.exe", "/c", "exit 7")
	results := make(chan pipeResult, 1)
	results <- pipeResult{}
	if _, err := awaitHelper(h, results, 10*time.Second); err == nil || !strings.Contains(err.Error(), "code 7") {
		t.Fatalf("want exit-code error, got %v", err)
	}
	_ = c.Wait()
}
