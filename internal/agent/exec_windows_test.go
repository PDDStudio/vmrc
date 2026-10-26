//go:build windows

package agent

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// TestShellCommandPreservesQuotes runs the real cmd.exe: a quoted argument must
// reach cmd unchanged (Go's default argument escaping would make it \"a b\").
func TestShellCommandPreservesQuotes(t *testing.T) {
	tests := []struct{ cmdline, want string }{
		{`echo "a b"`, `"a b"`},
		{`echo "x"&& echo "y z"`, "\"x\"\r\n\"y z\""},
	}
	for _, tt := range tests {
		out, err := shellCommand(context.Background(), tt.cmdline).CombinedOutput()
		if err != nil {
			t.Fatalf("%q: %v (output %q)", tt.cmdline, err, out)
		}
		if got := strings.TrimRight(string(out), "\r\n"); got != tt.want {
			t.Errorf("%q printed %q, want %q", tt.cmdline, got, tt.want)
		}
	}
}

func TestShellCommandExitStatus(t *testing.T) {
	err := shellCommand(context.Background(), `exit /b 3`).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want an *exec.ExitError, got %v", err)
	}
	if code := ee.ExitCode(); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
}
