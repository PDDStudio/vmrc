package host

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultFanoutTimeout bounds each agent's request on a fleet-wide exec when the
// operator hasn't set --timeout. It sits just above the agent's own 10-minute
// /exec ceiling, so it only ever trips for an unresponsive node — never for a
// command the agent would actually run to completion — yet stops one hung node
// from blocking `exec --all` forever.
const DefaultFanoutTimeout = 11 * time.Minute

// ExecResult is one agent's outcome from a fan-out exec.
type ExecResult struct {
	Name       string
	Out        []byte
	ExitStatus int
	Err        error
}

// FanoutExec runs cmdline on every agent concurrently, building a Client per
// agent via mkClient. Results are returned sorted by agent name. When timeout
// > 0 each agent runs under its own deadline, so a single reachable-but-hung
// node cannot block the aggregate beyond timeout; 0 means no per-agent bound.
func FanoutExec(ctx context.Context, agents []Agent, mkClient func(Agent) *Client, cmdline string, timeout time.Duration) []ExecResult {
	results := make([]ExecResult, len(agents))
	var wg sync.WaitGroup
	// One goroutine per agent, unbounded. Fine for practical fleet sizes; a
	// worker cap can be added if very large tailnets ever need it.
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a Agent) {
			defer wg.Done()
			aCtx := ctx
			if timeout > 0 {
				var cancel context.CancelFunc
				aCtx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			out, status, err := mkClient(a).Exec(aCtx, cmdline)
			results[i] = ExecResult{Name: a.Name, Out: out, ExitStatus: status, Err: err}
		}(i, a)
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results
}

// FormatExec renders results. With a single target (multi=false) output is
// unprefixed for clean piping; with multiple, each line is prefixed "[name] ".
// The aggregate exit code is 0 only if every result connected and exited 0;
// otherwise it is the first non-zero exit status, or 1 on a transport error.
func FormatExec(results []ExecResult, multi bool) (string, int) {
	var b strings.Builder
	code := 0
	for _, r := range results {
		if r.Err != nil {
			if multi {
				fmt.Fprintf(&b, "[%s] error: %v\n", r.Name, r.Err)
			} else {
				fmt.Fprintf(&b, "error: %v\n", r.Err)
			}
			if code == 0 {
				code = 1
			}
			continue
		}
		if r.ExitStatus != 0 && code == 0 {
			code = r.ExitStatus
		}
		writeLabeled(&b, r.Name, r.Out, multi)
	}
	return b.String(), code
}

// SummarizeExec returns a one-line-per-agent summary of a fan-out exec result
// set, intended for stderr when more than one target is addressed. Each line is
// "name: exit N" for a completed command, or "name: error: ..." for a transport
// failure.
func SummarizeExec(results []ExecResult) string {
	var b strings.Builder
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(&b, "%s: error: %v\n", r.Name, r.Err)
		} else {
			fmt.Fprintf(&b, "%s: exit %d\n", r.Name, r.ExitStatus)
		}
	}
	return b.String()
}

func writeLabeled(b *strings.Builder, name string, out []byte, multi bool) {
	if !multi {
		b.Write(out)
		return
	}
	if len(out) == 0 {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		fmt.Fprintf(b, "[%s] %s\n", name, line)
	}
}
