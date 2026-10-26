package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pddstudio/vmrc/internal/host"
	"github.com/pddstudio/vmrc/internal/mcpserver"
	"github.com/urfave/cli/v3"
)

func mcpCommand() *cli.Command {
	return &cli.Command{
		Name:  "mcp",
		Usage: "serve the vmrc verbs as MCP tools over stdio (for Claude Code / MCP clients)",
		Description: "Runs a Model Context Protocol server on stdin/stdout exposing vmrc_ls, " +
			"vmrc_exec, vmrc_screenshot, vmrc_click, vmrc_type, vmrc_key, vmrc_get and vmrc_put. " +
			"The global flags (--token/--port/--scheme/--tag/--timeout) apply to every tool call. " +
			"Self-update is intentionally not exposed. Logs go to stderr; stdout carries only the protocol.\n\n" +
			"Register with Claude Code:  claude mcp add vmrc -- vmrc mcp",
		Action: mcpAction,
	}
}

func mcpAction(ctx context.Context, cmd *cli.Command) error {
	// stdout is the JSON-RPC channel: every diagnostic must go to stderr.
	logger := log.New(os.Stderr, "vmrc mcp: ", log.LstdFlags)
	logger.Printf("serving MCP over stdio (version %s, tag %s)", version, cmd.String("tag"))
	err := mcpserver.Run(ctx, mcpserver.Config{
		Discoverer: discoverer(),
		NewClient:  func(a host.Agent) *host.Client { return clientFor(cmd, a) },
		Tag:        cmd.String("tag"),
		Version:    version,
		Logger:     logger,
	}, &mcp.StdioTransport{})
	if err != nil && ctx.Err() != nil {
		// Interrupted (SIGINT/SIGTERM): a normal shutdown, not a failure.
		return nil
	}
	return err
}
