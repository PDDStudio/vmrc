package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pddstudio/vmrc/internal/host"
)

// fakePNG is a recognisable stand-in for a screenshot body.
var fakePNG = []byte("\x89PNG\r\n\x1a\nfake-image-bytes")

// fakeAgent is an httptest stand-in for vmrc-agent's HTTP API. It records the
// last input/upload so tests can assert what the tools sent.
type fakeAgent struct {
	mu     sync.Mutex
	files  map[string][]byte
	clicks []string
	typed  []string
	keys   []string
}

func newFakeAgent() *fakeAgent {
	return &fakeAgent{files: map[string][]byte{
		"notes.txt":  []byte("hello from the vm\n"),
		"binary.bin": {0xff, 0xfe, 0x00, 0x01},
		"big.txt":    bytes.Repeat([]byte("a"), maxInlineBytes+10),
	}}
}

func (f *fakeAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	switch r.URL.Path {
	case "/exec":
		cmd := q.Get("cmd")
		switch {
		case strings.HasPrefix(cmd, "echo "):
			w.Header().Set("X-Exit-Status", "0")
			_, _ = io.WriteString(w, strings.TrimPrefix(cmd, "echo ")+"\n")
		case cmd == "false":
			w.Header().Set("X-Exit-Status", "1")
			_, _ = io.WriteString(w, "boom\n")
		case cmd == "sleep":
			<-r.Context().Done()
		default:
			w.Header().Set("X-Exit-Status", "127")
			_, _ = io.WriteString(w, "not found\n")
		}
	case "/screenshot":
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakePNG)
	case "/click":
		f.clicks = append(f.clicks, fmt.Sprintf("%s,%s,%s,%s", q.Get("x"), q.Get("y"), q.Get("button"), q.Get("double")))
	case "/type":
		f.typed = append(f.typed, q.Get("text"))
	case "/key":
		f.keys = append(f.keys, q.Get("combo"))
	case "/get":
		b, ok := f.files[q.Get("path")]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
	case "/put":
		b, _ := io.ReadAll(r.Body)
		f.files[q.Get("path")] = b
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAgent) file(path string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.files[path]
	return b, ok
}

// fakeDiscoverer serves a fixed agent list.
type fakeDiscoverer struct {
	agents []host.Agent
	err    error
}

func (d fakeDiscoverer) List(context.Context, string) ([]host.Agent, error) {
	return d.agents, d.err
}

func (d fakeDiscoverer) Resolve(_ context.Context, name, _ string) (host.Agent, error) {
	if d.err != nil {
		return host.Agent{}, d.err
	}
	for _, a := range d.agents {
		if a.Name == name || a.DNSName == name {
			return a, nil
		}
	}
	return host.Agent{}, fmt.Errorf("no vmrc agent named %q", name)
}

type fixture struct {
	agent *fakeAgent
	cs    *mcp.ClientSession
}

// setup starts a fake agent ("vm1"), a 403-only agent ("locked"), an
// unreachable agent ("down"), and an MCP client/server pair over the SDK's
// in-memory transport.
func setup(t *testing.T, disc *fakeDiscoverer) *fixture {
	t.Helper()
	fa := newFakeAgent()
	okSrv := httptest.NewServer(fa)
	t.Cleanup(okSrv.Close)
	lockedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden: identity not allowlisted", http.StatusForbidden)
	}))
	t.Cleanup(lockedSrv.Close)
	downSrv := httptest.NewServer(http.NotFoundHandler())
	downURL := downSrv.URL
	downSrv.Close() // closed: connections are refused

	urls := map[string]string{"vm1": okSrv.URL, "locked": lockedSrv.URL, "down": downURL}
	if disc == nil {
		disc = &fakeDiscoverer{agents: []host.Agent{
			{Name: "down", DNSName: "down.example.ts.net", OS: "linux", Online: false},
			{Name: "locked", DNSName: "locked.example.ts.net", OS: "windows", Online: true},
			{Name: "vm1", DNSName: "vm1.example.ts.net", OS: "windows", Online: true},
		}}
	}
	cfg := Config{
		Discoverer: disc,
		NewClient: func(a host.Agent) *host.Client {
			return &host.Client{BaseURL: urls[a.Name], HTTP: &http.Client{}}
		},
		Version: "test",
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := New(cfg).Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &fixture{agent: fa, cs: cs}
}

func (f *fixture) call(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := f.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) protocol error: %v", tool, err)
	}
	return res
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func mustOK(t *testing.T, res *mcp.CallToolResult) {
	t.Helper()
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}
}

func mustErr(t *testing.T, res *mcp.CallToolResult, want string) {
	t.Helper()
	if !res.IsError {
		t.Fatalf("expected tool error containing %q, got success: %s", want, textOf(t, res))
	}
	if got := textOf(t, res); !strings.Contains(got, want) {
		t.Fatalf("tool error = %q, want it to contain %q", got, want)
	}
}

func TestListToolsExposesVerbsButNotUpdate(t *testing.T) {
	f := setup(t, nil)
	res, err := f.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}
	want := []string{"vmrc_ls", "vmrc_exec", "vmrc_screenshot", "vmrc_click", "vmrc_type", "vmrc_key", "vmrc_get", "vmrc_put"}
	if len(got) != len(want) {
		t.Errorf("got %d tools, want %d", len(got), len(want))
	}
	for _, name := range want {
		tool, ok := got[name]
		if !ok {
			t.Errorf("missing tool %s", name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("%s has no description", name)
		}
	}
	for name := range got {
		if strings.Contains(name, "update") {
			t.Errorf("self-update must not be exposed as a tool, found %s", name)
		}
	}

	// Input schemas carry the typed fields, descriptions and required lists.
	schema, _ := json.Marshal(got["vmrc_exec"].InputSchema)
	for _, frag := range []string{`"vm"`, `"command"`, `"timeout_seconds"`, `"required":["vm","command"]`, `agent name from vmrc_ls`} {
		if !strings.Contains(string(schema), frag) {
			t.Errorf("vmrc_exec schema missing %s: %s", frag, schema)
		}
	}
}

func TestLs(t *testing.T) {
	f := setup(t, nil)
	res := f.call(t, "vmrc_ls", nil)
	mustOK(t, res)
	var out LsOutput
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Agents) != 3 || out.Agents[2].Name != "vm1" || out.Agents[2].OS != "windows" || !out.Agents[2].Online {
		t.Fatalf("agents = %+v", out.Agents)
	}
	if !strings.Contains(textOf(t, res), "vm1.example.ts.net") {
		t.Fatalf("text content should carry the JSON listing, got %q", textOf(t, res))
	}
}

func TestLsEmptyFleetIsEmptyArray(t *testing.T) {
	f := setup(t, &fakeDiscoverer{})
	res := f.call(t, "vmrc_ls", nil)
	mustOK(t, res)
	if !strings.Contains(textOf(t, res), `"agents":[]`) {
		t.Fatalf("want an empty agents array, got %q", textOf(t, res))
	}
}

func TestLsDiscoveryFailureIsToolError(t *testing.T) {
	f := setup(t, &fakeDiscoverer{err: fmt.Errorf("could not reach the local Tailscale daemon")})
	mustErr(t, f.call(t, "vmrc_ls", nil), "Tailscale daemon")
}

func TestExec(t *testing.T) {
	f := setup(t, nil)
	res := f.call(t, "vmrc_exec", map[string]any{"vm": "vm1", "command": "echo hi there"})
	mustOK(t, res)
	if got := textOf(t, res); got != "hi there\n[exit code 0]" {
		t.Fatalf("text = %q", got)
	}
	out := res.StructuredContent.(map[string]any)
	if out["output"] != "hi there\n" || out["exit_code"] != float64(0) {
		t.Fatalf("structured = %v", out)
	}
}

func TestExecNonZeroExitIsNotAToolError(t *testing.T) {
	f := setup(t, nil)
	res := f.call(t, "vmrc_exec", map[string]any{"vm": "vm1", "command": "false"})
	mustOK(t, res)
	if got := textOf(t, res); !strings.Contains(got, "boom") || !strings.Contains(got, "[exit code 1]") {
		t.Fatalf("text = %q", got)
	}
}

func TestExecTimeout(t *testing.T) {
	f := setup(t, nil)
	mustErr(t, f.call(t, "vmrc_exec", map[string]any{"vm": "vm1", "command": "sleep", "timeout_seconds": 1}), "timed out after 1s")
}

func TestExecValidation(t *testing.T) {
	f := setup(t, nil)
	mustErr(t, f.call(t, "vmrc_exec", map[string]any{"vm": "vm1", "command": "  "}), `"command" is required`)
	mustErr(t, f.call(t, "vmrc_exec", map[string]any{"vm": "vm1", "command": "x", "timeout_seconds": -1}), "timeout_seconds")
	mustErr(t, f.call(t, "vmrc_exec", map[string]any{"vm": "", "command": "x"}), "vmrc_ls")
}

func TestErrorPathsAreToolErrors(t *testing.T) {
	f := setup(t, nil)
	mustErr(t, f.call(t, "vmrc_exec", map[string]any{"vm": "nope", "command": "echo x"}), `no vmrc agent named "nope"`)
	mustErr(t, f.call(t, "vmrc_exec", map[string]any{"vm": "locked", "command": "echo x"}), "403")
	mustErr(t, f.call(t, "vmrc_screenshot", map[string]any{"vm": "locked"}), "not allowlisted")
	mustErr(t, f.call(t, "vmrc_exec", map[string]any{"vm": "down", "command": "echo x"}), "vmrc_exec on down")
	// The session survives all of the above.
	mustOK(t, f.call(t, "vmrc_exec", map[string]any{"vm": "vm1", "command": "echo still alive"}))
}

func TestMissingRequiredArgumentRejected(t *testing.T) {
	f := setup(t, nil)
	// Schema validation in the SDK rejects a call without the required "vm".
	res, err := f.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "vmrc_screenshot", Arguments: map[string]any{}})
	if err == nil && !res.IsError {
		t.Fatal("expected missing vm to be rejected")
	}
}

func TestScreenshotReturnsPNGImage(t *testing.T) {
	f := setup(t, nil)
	res := f.call(t, "vmrc_screenshot", map[string]any{"vm": "vm1"})
	mustOK(t, res)
	var img *mcp.ImageContent
	for _, c := range res.Content {
		if ic, ok := c.(*mcp.ImageContent); ok {
			img = ic
		}
	}
	if img == nil {
		t.Fatalf("no image content in %+v", res.Content)
	}
	if img.MIMEType != "image/png" || !bytes.Equal(img.Data, fakePNG) {
		t.Fatalf("image = %s %q", img.MIMEType, img.Data)
	}
}

func TestInputVerbs(t *testing.T) {
	f := setup(t, nil)
	mustOK(t, f.call(t, "vmrc_click", map[string]any{"vm": "vm1", "x": 10, "y": 20}))
	mustOK(t, f.call(t, "vmrc_click", map[string]any{"vm": "vm1", "x": 1, "y": 2, "button": "right", "double": true}))
	mustErr(t, f.call(t, "vmrc_click", map[string]any{"vm": "vm1", "x": 1, "y": 2, "button": "side"}), "left, right, or middle")
	mustOK(t, f.call(t, "vmrc_type", map[string]any{"vm": "vm1", "text": "héllo"}))
	mustErr(t, f.call(t, "vmrc_type", map[string]any{"vm": "vm1", "text": ""}), `"text" is required`)
	res := f.call(t, "vmrc_key", map[string]any{"vm": "vm1", "combo": "Ctrl+C"})
	mustOK(t, res)
	if textOf(t, res) != "pressed Ctrl+C on vm1" {
		t.Fatalf("key text = %q", textOf(t, res))
	}

	f.agent.mu.Lock()
	defer f.agent.mu.Unlock()
	if want := []string{"10,20,left,", "1,2,right,1"}; strings.Join(f.agent.clicks, "|") != strings.Join(want, "|") {
		t.Errorf("clicks = %v, want %v", f.agent.clicks, want)
	}
	if len(f.agent.typed) != 1 || f.agent.typed[0] != "héllo" {
		t.Errorf("typed = %v", f.agent.typed)
	}
	if len(f.agent.keys) != 1 || f.agent.keys[0] != "Ctrl+C" {
		t.Errorf("keys = %v", f.agent.keys)
	}
}

func TestGetInlineText(t *testing.T) {
	f := setup(t, nil)
	res := f.call(t, "vmrc_get", map[string]any{"vm": "vm1", "remote_path": "notes.txt"})
	mustOK(t, res)
	if textOf(t, res) != "hello from the vm\n" {
		t.Fatalf("text = %q", textOf(t, res))
	}
}

func TestGetInlineRejectsBinaryAndOversize(t *testing.T) {
	f := setup(t, nil)
	mustErr(t, f.call(t, "vmrc_get", map[string]any{"vm": "vm1", "remote_path": "binary.bin"}), "local_path")
	mustErr(t, f.call(t, "vmrc_get", map[string]any{"vm": "vm1", "remote_path": "big.txt"}), "larger than 1 MiB")
	mustErr(t, f.call(t, "vmrc_get", map[string]any{"vm": "vm1", "remote_path": "missing.txt"}), "404")
}

func TestGetToLocalPath(t *testing.T) {
	f := setup(t, nil)
	local := filepath.Join(t.TempDir(), "out.bin")
	res := f.call(t, "vmrc_get", map[string]any{"vm": "vm1", "remote_path": "binary.bin", "local_path": local})
	mustOK(t, res)
	got, err := os.ReadFile(local)
	if err != nil || !bytes.Equal(got, []byte{0xff, 0xfe, 0x00, 0x01}) {
		t.Fatalf("local file = %q, %v", got, err)
	}
	if !strings.Contains(textOf(t, res), "wrote 4 bytes") {
		t.Fatalf("text = %q", textOf(t, res))
	}
	// Oversize files are fine with local_path.
	big := filepath.Join(t.TempDir(), "big.txt")
	mustOK(t, f.call(t, "vmrc_get", map[string]any{"vm": "vm1", "remote_path": "big.txt", "local_path": big}))
	if fi, err := os.Stat(big); err != nil || fi.Size() != maxInlineBytes+10 {
		t.Fatalf("big download: %v %v", fi, err)
	}
}

func TestGetFailureLeavesExistingLocalFile(t *testing.T) {
	f := setup(t, nil)
	local := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(local, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustErr(t, f.call(t, "vmrc_get", map[string]any{"vm": "vm1", "remote_path": "missing.txt", "local_path": local}), "404")
	if got, _ := os.ReadFile(local); string(got) != "ORIGINAL" {
		t.Fatalf("existing file clobbered: %q", got)
	}
}

func TestPutContent(t *testing.T) {
	f := setup(t, nil)
	res := f.call(t, "vmrc_put", map[string]any{"vm": "vm1", "remote_path": "new.txt", "content": "fresh"})
	mustOK(t, res)
	if b, ok := f.agent.file("new.txt"); !ok || string(b) != "fresh" {
		t.Fatalf("uploaded = %q (%t)", b, ok)
	}
	// Empty content is a valid (empty) file, distinct from "no content".
	mustOK(t, f.call(t, "vmrc_put", map[string]any{"vm": "vm1", "remote_path": "empty.txt", "content": ""}))
	if b, ok := f.agent.file("empty.txt"); !ok || len(b) != 0 {
		t.Fatalf("empty upload = %q (%t)", b, ok)
	}
}

func TestPutLocalPath(t *testing.T) {
	f := setup(t, nil)
	local := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(local, []byte("from-host"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := f.call(t, "vmrc_put", map[string]any{"vm": "vm1", "remote_path": "dst.bin", "local_path": local})
	mustOK(t, res)
	if b, _ := f.agent.file("dst.bin"); string(b) != "from-host" {
		t.Fatalf("uploaded = %q", b)
	}
	if !strings.Contains(textOf(t, res), "uploaded 9 bytes to vm1:dst.bin") {
		t.Fatalf("text = %q", textOf(t, res))
	}
}

func TestPutValidation(t *testing.T) {
	f := setup(t, nil)
	mustErr(t, f.call(t, "vmrc_put", map[string]any{"vm": "vm1", "remote_path": "x"}), "exactly one")
	mustErr(t, f.call(t, "vmrc_put", map[string]any{"vm": "vm1", "remote_path": "x", "content": "a", "local_path": "/tmp/y"}), "exactly one")
	mustErr(t, f.call(t, "vmrc_put", map[string]any{"vm": "vm1", "remote_path": "x", "local_path": filepath.Join(t.TempDir(), "missing")}), "open ")
	mustErr(t, f.call(t, "vmrc_put", map[string]any{"vm": "vm1", "remote_path": "x", "local_path": t.TempDir()}), "is a directory")
	mustErr(t, f.call(t, "vmrc_put", map[string]any{"vm": "locked", "remote_path": "x", "content": "a"}), "403")
}
