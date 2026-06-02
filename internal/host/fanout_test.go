package host

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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

func TestFanoutExecRunsAllAndSorts(t *testing.T) {
	agents := []Agent{{Name: "vm2"}, {Name: "vm1"}}
	// mkClient returns a client whose Exec is stubbed via a test server-less path:
	// here we exercise ordering only, using a client pointed at an unreachable URL,
	// so every result carries an Err but the Name/order is what we assert.
	results := FanoutExec(context.Background(), agents,
		func(a Agent) *Client { return &Client{BaseURL: "http://127.0.0.1:0"} }, "x")
	if len(results) != 2 || results[0].Name != "vm1" || results[1].Name != "vm2" {
		t.Fatalf("results not sorted by name: %v", results)
	}
}
