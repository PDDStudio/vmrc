// Package mcpserver exposes the vmrc host verbs as Model Context Protocol tools
// so an AI agent (Claude Code, Claude Desktop, any MCP client) can drive a fleet
// of vmrc agents without shelling out to the CLI. It reuses the host package's
// discovery and HTTP verb client; cmd/vmrc only wires it to a stdio transport.
//
// Self-update is deliberately NOT exposed: replacing agent binaries is an
// operator decision, not something an autonomous model should trigger.
package mcpserver

import (
	"context"
	"io"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pddstudio/vmrc/internal/host"
)

// Discoverer enumerates and resolves vmrc agents. host.Discoverer satisfies it;
// tests inject a fake that points at an httptest agent.
type Discoverer interface {
	List(ctx context.Context, tag string) ([]host.Agent, error)
	Resolve(ctx context.Context, name, tag string) (host.Agent, error)
}

// Config wires the MCP server to the fleet.
type Config struct {
	// Discoverer finds agents (required).
	Discoverer Discoverer
	// NewClient builds a verb client for a resolved agent (required). The CLI
	// passes its clientFor closure so --token/--port/--scheme/--timeout apply.
	NewClient func(host.Agent) *host.Client
	// Tag is the tailnet ACL tag identifying agents ("" = host.DefaultTag).
	Tag string
	// Version is reported to MCP clients in the initialize handshake.
	Version string
	// Logger receives one line per tool call. It must not write to stdout when
	// serving over stdio (stdout is the protocol channel). nil discards logs.
	Logger *log.Logger
}

// instructions is sent to MCP clients at initialize time as usage guidance.
const instructions = `vmrc drives VMs ("agents") on the operator's tailnet.
Start with vmrc_ls to see agent names; every other tool takes one agent name as "vm".
GUI loop: vmrc_screenshot to see the screen, then vmrc_click / vmrc_type / vmrc_key, then screenshot again to verify.
vmrc_exec runs a shell command line (sh -c on Linux/macOS, cmd /c on Windows); a non-zero exit code is reported, not raised.
vmrc_get / vmrc_put transfer files; remote paths are relative to the agent's base directory and cannot escape it.`

// New builds an MCP server with every vmrc tool registered.
func New(cfg Config) *mcp.Server {
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "vmrc", Title: "vmrc VM remote control", Version: cfg.Version},
		&mcp.ServerOptions{
			Instructions: instructions,
			// Empty (non-nil) capabilities drop the SDK's historical default
			// "logging" capability: this server sends no MCP log messages (it logs
			// to stderr). "tools" is still inferred from the registered tools.
			Capabilities: &mcp.ServerCapabilities{},
		})
	t := &tools{cfg: cfg}
	t.register(s)
	return s
}

// Run serves the MCP protocol over transport until the client disconnects or
// ctx is cancelled.
func Run(ctx context.Context, cfg Config, transport mcp.Transport) error {
	return New(cfg).Run(ctx, transport)
}
