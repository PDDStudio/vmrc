package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pddstudio/vmrc/internal/atomicfile"
	"github.com/pddstudio/vmrc/internal/host"
)

// maxInlineBytes caps content returned inline to the model (vmrc_get without
// local_path, vmrc_exec output). Larger payloads would blow the model's context
// window; vmrc_get points at local_path instead, vmrc_exec truncates.
const maxInlineBytes = 1 << 20 // 1 MiB

// tools holds the shared config; each method is one MCP tool handler.
type tools struct {
	cfg Config
}

func ptr[T any](v T) *T { return &v }

func (t *tools) register(s *mcp.Server) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)}
	mutating := &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(true)}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "vmrc_ls",
		Title:       "List VMs",
		Description: "List the vmrc agents (VMs) on the tailnet with their name, DNS name, OS and online state. Use a returned name as the \"vm\" argument of the other vmrc tools.",
		Annotations: readOnly,
	}, t.ls)
	mcp.AddTool(s, &mcp.Tool{
		Name:  "vmrc_exec",
		Title: "Run a command on a VM",
		Description: "Run a shell command line on a VM via its platform shell (sh -c on Linux/macOS, cmd /c on Windows) in the agent's base directory, " +
			"and return combined stdout+stderr and the exit code. A non-zero exit code is a normal result, not a tool error. " +
			"The command waits for completion (agent cap: 10 minutes); output beyond 1 MiB is truncated.",
		Annotations: mutating,
	}, t.exec)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "vmrc_screenshot",
		Title:       "Screenshot a VM",
		Description: "Capture the VM's screen and return it as a PNG image, so you can see the desktop before clicking or typing. Coordinates for vmrc_click are absolute pixels in this image.",
		Annotations: readOnly,
	}, t.screenshot)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "vmrc_click",
		Title:       "Click on a VM",
		Description: "Click the mouse at absolute screen pixel (x, y) on a VM (coordinates as seen in vmrc_screenshot). Take a screenshot afterwards to verify the effect.",
		Annotations: mutating,
	}, t.click)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "vmrc_type",
		Title:       "Type text on a VM",
		Description: "Type literal text on a VM as Unicode keystrokes into the focused window. It does not press Enter; use vmrc_key for that.",
		Annotations: mutating,
	}, t.typeText)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "vmrc_key",
		Title:       "Press a key combo on a VM",
		Description: "Press a key or key combination on a VM, e.g. \"Enter\", \"Tab\", \"Esc\", \"Ctrl+C\", \"Alt+F4\", \"Ctrl+Shift+Esc\".",
		Annotations: mutating,
	}, t.key)
	mcp.AddTool(s, &mcp.Tool{
		Name:  "vmrc_get",
		Title: "Download a file from a VM",
		Description: "Download a file from a VM. remote_path is relative to the agent's base directory. " +
			"With local_path the file is written atomically on this host and its size is returned; " +
			"without it the content is returned as text if it is valid UTF-8 and at most 1 MiB (otherwise pass local_path).",
		// Not read-only: with local_path it creates/overwrites a file on this host.
		Annotations: mutating,
	}, t.get)
	mcp.AddTool(s, &mcp.Tool{
		Name:  "vmrc_put",
		Title: "Upload a file to a VM",
		Description: "Upload a file to a VM at remote_path (relative to the agent's base directory), overwriting it. " +
			"Provide exactly one of local_path (a file on this host) or content (literal text).",
		Annotations: mutating,
	}, t.put)
}

// --- shared helpers ---------------------------------------------------------

// client resolves vm on the tailnet and builds its verb client.
func (t *tools) client(ctx context.Context, vm string) (host.Agent, *host.Client, error) {
	if strings.TrimSpace(vm) == "" {
		return host.Agent{}, nil, errors.New(`"vm" is required; call vmrc_ls to list agent names`)
	}
	a, err := t.cfg.Discoverer.Resolve(ctx, vm, t.cfg.Tag)
	if err != nil {
		return host.Agent{}, nil, err
	}
	return a, t.cfg.NewClient(a), nil
}

// fail logs and returns a tool error; the SDK turns a handler error into a
// CallToolResult with IsError set, so the model sees the message and can
// self-correct instead of the session dying.
func (t *tools) fail(tool, vm string, err error) error {
	t.cfg.Logger.Printf("%s vm=%q: error: %v", tool, vm, err)
	if vm != "" {
		return fmt.Errorf("%s on %s: %w", tool, vm, err)
	}
	return fmt.Errorf("%s: %w", tool, err)
}

func text(format string, a ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, a...)}}}
}

// --- vmrc_ls ----------------------------------------------------------------

// LsInput takes no arguments.
type LsInput struct{}

// AgentInfo describes one discovered agent.
type AgentInfo struct {
	Name    string `json:"name" jsonschema:"short name; pass it as vm to the other tools"`
	DNSName string `json:"dns_name" jsonschema:"MagicDNS FQDN"`
	OS      string `json:"os,omitempty" jsonschema:"operating system reported by Tailscale"`
	Online  bool   `json:"online" jsonschema:"whether the node is currently online on the tailnet"`
}

// LsOutput is the structured result of vmrc_ls.
type LsOutput struct {
	Agents []AgentInfo `json:"agents" jsonschema:"discovered vmrc agents, sorted by name"`
}

func (t *tools) ls(ctx context.Context, _ *mcp.CallToolRequest, _ LsInput) (*mcp.CallToolResult, LsOutput, error) {
	t.cfg.Logger.Printf("vmrc_ls")
	agents, err := t.cfg.Discoverer.List(ctx, t.cfg.Tag)
	if err != nil {
		return nil, LsOutput{}, t.fail("vmrc_ls", "", err)
	}
	out := LsOutput{Agents: make([]AgentInfo, 0, len(agents))}
	for _, a := range agents {
		out.Agents = append(out.Agents, AgentInfo{Name: a.Name, DNSName: a.DNSName, OS: a.OS, Online: a.Online})
	}
	// Content left nil: the SDK fills it with the JSON of out.
	return nil, out, nil
}

// --- vmrc_exec --------------------------------------------------------------

// ExecInput is the argument of vmrc_exec.
type ExecInput struct {
	VM             string `json:"vm" jsonschema:"agent name from vmrc_ls"`
	Command        string `json:"command" jsonschema:"command line run via sh -c (Linux/macOS) or cmd /c (Windows); pipes and redirection work"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"optional client-side timeout in seconds; the command is cancelled when it elapses (0 = wait up to the agent's 10 minute cap)"`
}

// ExecOutput is the structured result of vmrc_exec.
type ExecOutput struct {
	Output    string `json:"output" jsonschema:"combined stdout and stderr"`
	ExitCode  int    `json:"exit_code" jsonschema:"process exit code; -1 if it could not be started or was killed"`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"true when output exceeded 1 MiB and was cut"`
}

func (t *tools) exec(ctx context.Context, _ *mcp.CallToolRequest, in ExecInput) (*mcp.CallToolResult, ExecOutput, error) {
	const name = "vmrc_exec"
	t.cfg.Logger.Printf("%s vm=%q cmd=%q", name, in.VM, in.Command)
	if strings.TrimSpace(in.Command) == "" {
		return nil, ExecOutput{}, t.fail(name, in.VM, errors.New(`"command" is required`))
	}
	if in.TimeoutSeconds < 0 {
		return nil, ExecOutput{}, t.fail(name, in.VM, errors.New(`"timeout_seconds" must be >= 0`))
	}
	_, c, err := t.client(ctx, in.VM)
	if err != nil {
		return nil, ExecOutput{}, t.fail(name, in.VM, err)
	}
	if in.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	raw, code, err := c.Exec(ctx, in.Command)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("timed out after %ds and was cancelled: %w", in.TimeoutSeconds, err)
		}
		return nil, ExecOutput{}, t.fail(name, in.VM, err)
	}
	out := ExecOutput{ExitCode: code}
	if len(raw) > maxInlineBytes {
		raw, out.Truncated = raw[:maxInlineBytes], true
	}
	out.Output = strings.ToValidUTF8(string(raw), "�")
	t.cfg.Logger.Printf("%s vm=%q exit=%d (%d bytes)", name, in.VM, code, len(raw))

	var b strings.Builder
	b.WriteString(out.Output)
	if out.Output != "" && !strings.HasSuffix(out.Output, "\n") {
		b.WriteByte('\n')
	}
	if out.Truncated {
		b.WriteString("[output truncated at 1 MiB]\n")
	}
	fmt.Fprintf(&b, "[exit code %d]", code)
	return text("%s", b.String()), out, nil
}

// --- vmrc_screenshot --------------------------------------------------------

// VMInput is the argument of tools that take only a target.
type VMInput struct {
	VM string `json:"vm" jsonschema:"agent name from vmrc_ls"`
}

func (t *tools) screenshot(ctx context.Context, _ *mcp.CallToolRequest, in VMInput) (*mcp.CallToolResult, any, error) {
	const name = "vmrc_screenshot"
	t.cfg.Logger.Printf("%s vm=%q", name, in.VM)
	a, c, err := t.client(ctx, in.VM)
	if err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	png, err := c.Screenshot(ctx)
	if err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.ImageContent{Data: png, MIMEType: "image/png"},
		&mcp.TextContent{Text: fmt.Sprintf("screenshot of %s (%d bytes PNG)", a.Name, len(png))},
	}}, nil, nil
}

// --- vmrc_click / vmrc_type / vmrc_key ---------------------------------------

// ClickInput is the argument of vmrc_click.
type ClickInput struct {
	VM     string `json:"vm" jsonschema:"agent name from vmrc_ls"`
	X      int    `json:"x" jsonschema:"absolute screen x pixel (as in vmrc_screenshot)"`
	Y      int    `json:"y" jsonschema:"absolute screen y pixel (as in vmrc_screenshot)"`
	Button string `json:"button,omitempty" jsonschema:"mouse button: left (default), right, or middle"`
	Double bool   `json:"double,omitempty" jsonschema:"double-click instead of a single click"`
}

func (t *tools) click(ctx context.Context, _ *mcp.CallToolRequest, in ClickInput) (*mcp.CallToolResult, any, error) {
	const name = "vmrc_click"
	t.cfg.Logger.Printf("%s vm=%q (%d,%d) button=%q double=%t", name, in.VM, in.X, in.Y, in.Button, in.Double)
	button := in.Button
	if button == "" {
		button = "left"
	}
	switch button {
	case "left", "right", "middle":
	default:
		return nil, nil, t.fail(name, in.VM, fmt.Errorf("button must be left, right, or middle, got %q", in.Button))
	}
	a, c, err := t.client(ctx, in.VM)
	if err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	if err := c.Click(ctx, in.X, in.Y, button, in.Double); err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	kind := "clicked"
	if in.Double {
		kind = "double-clicked"
	}
	return text("%s %s at (%d,%d) on %s", kind, button, in.X, in.Y, a.Name), nil, nil
}

// TypeInput is the argument of vmrc_type.
type TypeInput struct {
	VM   string `json:"vm" jsonschema:"agent name from vmrc_ls"`
	Text string `json:"text" jsonschema:"literal text to type into the focused window"`
}

func (t *tools) typeText(ctx context.Context, _ *mcp.CallToolRequest, in TypeInput) (*mcp.CallToolResult, any, error) {
	const name = "vmrc_type"
	n := utf8.RuneCountInString(in.Text)
	// Log the length only: typed text is often a password or secret.
	t.cfg.Logger.Printf("%s vm=%q %d chars", name, in.VM, n)
	if in.Text == "" {
		return nil, nil, t.fail(name, in.VM, errors.New(`"text" is required`))
	}
	a, c, err := t.client(ctx, in.VM)
	if err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	if err := c.Type(ctx, in.Text); err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	return text("typed %d chars on %s", n, a.Name), nil, nil
}

// KeyInput is the argument of vmrc_key.
type KeyInput struct {
	VM    string `json:"vm" jsonschema:"agent name from vmrc_ls"`
	Combo string `json:"combo" jsonschema:"key or combo, e.g. Enter, Tab, Esc, Ctrl+C, Alt+F4"`
}

func (t *tools) key(ctx context.Context, _ *mcp.CallToolRequest, in KeyInput) (*mcp.CallToolResult, any, error) {
	const name = "vmrc_key"
	t.cfg.Logger.Printf("%s vm=%q combo=%q", name, in.VM, in.Combo)
	if strings.TrimSpace(in.Combo) == "" {
		return nil, nil, t.fail(name, in.VM, errors.New(`"combo" is required`))
	}
	a, c, err := t.client(ctx, in.VM)
	if err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	if err := c.Key(ctx, in.Combo); err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	return text("pressed %s on %s", in.Combo, a.Name), nil, nil
}

// --- vmrc_get / vmrc_put ----------------------------------------------------

// GetInput is the argument of vmrc_get.
type GetInput struct {
	VM         string `json:"vm" jsonschema:"agent name from vmrc_ls"`
	RemotePath string `json:"remote_path" jsonschema:"file path on the VM, relative to the agent's base directory"`
	LocalPath  string `json:"local_path,omitempty" jsonschema:"optional path on this host to save the file to (written atomically); omit to get the content back as text"`
}

func (t *tools) get(ctx context.Context, _ *mcp.CallToolRequest, in GetInput) (*mcp.CallToolResult, any, error) {
	const name = "vmrc_get"
	t.cfg.Logger.Printf("%s vm=%q remote=%q local=%q", name, in.VM, in.RemotePath, in.LocalPath)
	if in.RemotePath == "" {
		return nil, nil, t.fail(name, in.VM, errors.New(`"remote_path" is required`))
	}
	var local string
	if in.LocalPath != "" {
		abs, err := filepath.Abs(in.LocalPath)
		if err != nil {
			return nil, nil, t.fail(name, in.VM, err)
		}
		local = abs
	}
	a, c, err := t.client(ctx, in.VM)
	if err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	rc, err := c.Get(ctx, in.RemotePath)
	if err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	defer func() { _ = rc.Close() }()

	if local != "" {
		n, err := atomicfile.Write(local, rc)
		if err != nil {
			return nil, nil, t.fail(name, in.VM, fmt.Errorf("download to %s: %w", local, err))
		}
		return text("wrote %d bytes from %s:%s to %s", n, a.Name, in.RemotePath, local), nil, nil
	}

	// Inline: read at most maxInlineBytes+1 to detect an oversize file without
	// buffering all of it.
	data, err := io.ReadAll(io.LimitReader(rc, maxInlineBytes+1))
	if err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	if len(data) > maxInlineBytes {
		return nil, nil, t.fail(name, in.VM, fmt.Errorf("%s is larger than 1 MiB; pass local_path to save it to a file instead", in.RemotePath))
	}
	if !utf8.Valid(data) {
		return nil, nil, t.fail(name, in.VM, fmt.Errorf("%s is not valid UTF-8 text (binary file?); pass local_path to save it to a file instead", in.RemotePath))
	}
	return text("%s", data), nil, nil
}

// PutInput is the argument of vmrc_put.
type PutInput struct {
	VM         string  `json:"vm" jsonschema:"agent name from vmrc_ls"`
	RemotePath string  `json:"remote_path" jsonschema:"destination path on the VM, relative to the agent's base directory; overwritten if it exists"`
	LocalPath  string  `json:"local_path,omitempty" jsonschema:"file on this host to upload (exactly one of local_path or content)"`
	Content    *string `json:"content,omitempty" jsonschema:"literal text to upload as the file's content (exactly one of local_path or content)"`
}

func (t *tools) put(ctx context.Context, _ *mcp.CallToolRequest, in PutInput) (*mcp.CallToolResult, any, error) {
	const name = "vmrc_put"
	t.cfg.Logger.Printf("%s vm=%q remote=%q local=%q inline=%t", name, in.VM, in.RemotePath, in.LocalPath, in.Content != nil)
	if in.RemotePath == "" {
		return nil, nil, t.fail(name, in.VM, errors.New(`"remote_path" is required`))
	}
	if (in.LocalPath == "") == (in.Content == nil) {
		return nil, nil, t.fail(name, in.VM, errors.New(`provide exactly one of "local_path" or "content"`))
	}

	var src io.Reader
	var size int64
	if in.Content != nil {
		src, size = strings.NewReader(*in.Content), int64(len(*in.Content))
	} else {
		f, err := os.Open(in.LocalPath)
		if err != nil {
			return nil, nil, t.fail(name, in.VM, err)
		}
		defer func() { _ = f.Close() }()
		fi, err := f.Stat()
		if err != nil {
			return nil, nil, t.fail(name, in.VM, err)
		}
		if fi.IsDir() {
			return nil, nil, t.fail(name, in.VM, fmt.Errorf("%s is a directory", in.LocalPath))
		}
		src, size = f, fi.Size()
	}

	a, c, err := t.client(ctx, in.VM)
	if err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	if err := c.Put(ctx, in.RemotePath, src); err != nil {
		return nil, nil, t.fail(name, in.VM, err)
	}
	return text("uploaded %d bytes to %s:%s", size, a.Name, in.RemotePath), nil, nil
}
