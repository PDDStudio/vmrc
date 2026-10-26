package host

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFanoutTimeout(t *testing.T) {
	if got := FanoutTimeout(true, 0); got != DefaultFanoutTimeout {
		t.Fatalf("--all with no explicit timeout: got %v, want %v", got, DefaultFanoutTimeout)
	}
	if got := FanoutTimeout(false, 0); got != 0 {
		t.Fatalf("explicit target with no timeout must stay unbounded, got %v", got)
	}
	if got := FanoutTimeout(true, 5*time.Second); got != 5*time.Second {
		t.Fatalf("explicit timeout must win over the --all default, got %v", got)
	}
	if got := FanoutTimeout(false, 5*time.Second); got != 5*time.Second {
		t.Fatalf("explicit timeout must be honored, got %v", got)
	}
}

// Bug 1: empty output must produce no labeled lines at all.
func TestFormatExecEmptyOutputNoBlankLine(t *testing.T) {
	results := []ExecResult{
		{Name: "vm1", Out: nil, ExitStatus: 0},
		{Name: "vm2", Out: []byte(""), ExitStatus: 0},
	}
	out, code := FormatExec(results, true)
	if out != "" {
		t.Fatalf("empty output should produce no lines, got %q", out)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
}

// Bug 2: single-target transport error must not prefix with [name].
func TestFormatExecSingleTargetErrorUnprefixed(t *testing.T) {
	results := []ExecResult{{Name: "vm1", Err: errors.New("dial fail")}}
	out, code := FormatExec(results, false)
	if strings.Contains(out, "[") {
		t.Fatalf("single-target error must not contain '[', got %q", out)
	}
	if !strings.Contains(out, "error:") {
		t.Fatalf("single-target error must contain 'error:', got %q", out)
	}
	if code == 0 {
		t.Fatalf("code must be non-zero, got %d", code)
	}
}

func TestFormatExecSingleTargetUnprefixed(t *testing.T) {
	out, code := FormatExec([]ExecResult{{Name: "vm1", Out: []byte("hello\n"), ExitStatus: 0}}, false)
	if out != "hello\n" {
		t.Fatalf("single target should be unprefixed, got %q", out)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
}

func TestFormatExecMultiTargetLabeled(t *testing.T) {
	results := []ExecResult{
		{Name: "vm1", Out: []byte("a\nb\n"), ExitStatus: 0},
		{Name: "vm2", Out: []byte("c\n"), ExitStatus: 0},
	}
	out, _ := FormatExec(results, true)
	for _, want := range []string{"[vm1] a\n", "[vm1] b\n", "[vm2] c\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestFormatExecAggregatesExitCode(t *testing.T) {
	results := []ExecResult{
		{Name: "vm1", ExitStatus: 0},
		{Name: "vm2", ExitStatus: 7},
	}
	if _, code := FormatExec(results, true); code != 7 {
		t.Fatalf("aggregate code = %d, want 7", code)
	}
}

func TestFormatExecErrorSetsNonZero(t *testing.T) {
	results := []ExecResult{{Name: "vm1", Err: errors.New("dial fail")}}
	out, code := FormatExec(results, true)
	if code == 0 || !strings.Contains(out, "error") {
		t.Fatalf("error result must report and fail: out=%q code=%d", out, code)
	}
}

func TestSummarizeExec(t *testing.T) {
	results := []ExecResult{
		{Name: "vm1", ExitStatus: 0},
		{Name: "vm2", ExitStatus: 7},
		{Name: "vm3", Err: errors.New("dial fail")},
	}
	got := SummarizeExec(results)
	for _, want := range []string{"vm1: exit 0\n", "vm2: exit 7\n", "vm3: error: dial fail\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in summary:\n%s", want, got)
		}
	}
}

func TestFanoutExecRunsAllAndSorts(t *testing.T) {
	agents := []Agent{{Name: "vm2"}, {Name: "vm1"}}
	// mkClient returns a client whose Exec is stubbed via a test server-less path:
	// here we exercise ordering only, using a client pointed at an unreachable URL,
	// so every result carries an Err but the Name/order is what we assert.
	results := FanoutExec(context.Background(), agents,
		func(a Agent) *Client { return &Client{BaseURL: "http://127.0.0.1:0"} }, "x", 0)
	if len(results) != 2 || results[0].Name != "vm1" || results[1].Name != "vm2" {
		t.Fatalf("results not sorted by name: %v", results)
	}
}

// A reachable-but-hung agent must not block the fan-out beyond the per-agent
// timeout: FanoutExec applies its own deadline and returns an error for that
// agent rather than waiting forever.
func TestFanoutExecPerAgentTimeout(t *testing.T) {
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // never respond until the request is cancelled
	}))
	defer hung.Close()

	agents := []Agent{{Name: "vm1"}}
	done := make(chan []ExecResult, 1)
	go func() {
		done <- FanoutExec(context.Background(), agents,
			func(a Agent) *Client { return &Client{BaseURL: hung.URL} }, "x", 150*time.Millisecond)
	}()

	select {
	case results := <-done:
		if len(results) != 1 || results[0].Err == nil {
			t.Fatalf("hung agent should yield an error result, got %+v", results)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FanoutExec did not return: per-agent timeout not honored")
	}
}
