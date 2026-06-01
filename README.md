# vmrc — VM remote control

> **Status: handoff / pre-implementation.** This repo currently contains only the
> design + handoff documents. No code yet. See [KICKOFF_PROMPT.md](./KICKOFF_PROMPT.md)
> to start the implementation session.

## What this is

`vmrc` ships as **two binaries** built from one Go module:

**`vmrc-agent`** — you drop it onto a VM. It:

1. **Installs itself as a boot service** (Windows Service / launchd / systemd) so it runs in
   the background every time the machine starts.
2. **Joins your Tailscale tailnet by itself** (embedded `tsnet` — no system Tailscale install
   required on the VM), so the tunnel comes up with the process.
3. **Exposes a small, identity-gated HTTP API** for remote control: screenshot the desktop,
   read files/logs, run commands, spawn detached processes, and push/pull files.

**`vmrc`** — the **host-side CLI**: from your main machine you run `vmrc ls`,
`vmrc exec <vm> -- <cmd>`, `vmrc shot <vm>`, `vmrc cp …` to drive a *fleet* of VMs over the
tailnet. A later phase adds an **MCP server mode** (`vmrc mcp`) so AI agents drive VMs natively.

## Why it exists

It generalizes a proven internal control agent that let a human *and* an AI agent drive a
Windows GUI VM (screenshots, logs, running build tooling) over Tailscale without RDP or SSH.
That pattern proved broadly useful; `vmrc` extracts it into a standalone, multi-OS, multi-VM,
self-installing tool. See [CONCEPT.md](./CONCEPT.md).

## Mental model

```
  host machine (Mac)                         VM 1 (Windows)
  ┌─────────────────┐     tailnet (WG)     ┌──────────────────────────┐
  │ vmrc ls/exec/…  │◀───────────────────▶ │ vmrc-agent (boot service) │
  │ (CLI / MCP)     │   http://vm:47800    │   embeds tsnet  ──► tailnet│
  └─────────────────┘                      │   serves /exec /shot /…   │
          ▲                                └──────────────────────────┘
          │  same tailnet                   VM 2 (Linux), VM 3 (macOS) …
          └──────────────────────────────────────────────────────────────
```

Trust boundary = the tailnet. Tailscale ACLs decide who can reach an agent; the agent
authorizes callers by their Tailscale identity (and optionally a shared token).

## Documents

| File | What's in it |
|---|---|
| [CONCEPT.md](./CONCEPT.md) | The why, the proven origin, capabilities, non-goals |
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Technical design: agent, tunnel, service, host CLI, endpoints, security |
| [ROADMAP.md](./ROADMAP.md) | Phased implementation plan (P0→P6) with done-checks |
| [CLAUDE.md](./CLAUDE.md) | Project context + decisions for Claude Code in this repo |
| [KICKOFF_PROMPT.md](./KICKOFF_PROMPT.md) | Paste-ready prompt to open the build session |
| [docs/superpowers/specs/2026-06-01-vmrc-design.md](./docs/superpowers/specs/2026-06-01-vmrc-design.md) | Implementation-level design + phase plan (**authoritative for *how* we build**) |

## Decided direction (do not re-litigate)

- **Binaries:** two — `vmrc` (host CLI) + `vmrc-agent` (VM agent)
- **Language:** Go
- **CLI framework:** `urfave/cli/v3`
- **Config:** flags + env + JSON file
- **Tunnel:** embedded `tailscale.com/tsnet`
- **Target OS:** Windows, Linux, macOS
- **Host UX:** CLI now, MCP server later
- **Auto-start:** `kardianos/service`
- **Auth:** Tailnet identity (tsnet `WhoIs`) + optional shared token

Full rationale in [CLAUDE.md](./CLAUDE.md) and [ARCHITECTURE.md](./ARCHITECTURE.md).
