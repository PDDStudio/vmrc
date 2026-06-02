# CLAUDE.md — Project context for Claude Code

This file is read automatically by Claude Code when working in this repo. It captures
decisions already made so the agent doesn't re-litigate them.

## What this project is

`vmrc` is a standalone **VM remote-control** tool shipped as **two binaries** built from one
module:

- **`vmrc`** — the host-side CLI that drives a fleet of VMs over the tailnet (and, later, an MCP
  server mode via `vmrc mcp`).
- **`vmrc-agent`** — runs on a VM: serves a small identity-gated HTTP API (screenshot / read /
  exec / spawn / get / put), joins your Tailscale tailnet by itself (embedded `tsnet`), and
  installs itself as a boot service.

It is the generalization of a proven internal control agent. See [CONCEPT.md](./CONCEPT.md),
[ARCHITECTURE.md](./ARCHITECTURE.md), [ROADMAP.md](./ROADMAP.md). The implementation-level design
and phase plan are authoritative in
[docs/superpowers/specs/2026-06-01-vmrc-design.md](./docs/superpowers/specs/2026-06-01-vmrc-design.md).

**Status:** pre-implementation. This repo currently holds handoff docs + the design spec. Start
with the spec's P0.

## Decisions already made (do not re-litigate)

| Topic | Choice | Why |
|---|---|---|
| Binaries | **Two: `vmrc` (CLI) + `vmrc-agent` (agent)** | Distinct responsibilities, distinct build targets; shared logic in `internal/*` |
| Language | **Go** | Lifts the existing ~270-line control server almost verbatim; `tsnet` is Go-only |
| CLI framework | **`github.com/urfave/cli/v3`** | Subcommand tree + flag/env/file sources; approved this session |
| Config | **flags + env + JSON file** | Precedence flag → env → JSON file → default; secrets from env only |
| Tunnel | **Embed `tailscale.com/tsnet`** | The agent is its own tailnet node — no system Tailscale install on the VM; tunnel up on boot with the process |
| Target OS | **Windows, Linux, macOS** (not FreeBSD) | `kbinani/screenshot` + `kardianos/service` cover exactly these three |
| Auto-start | **`github.com/kardianos/service`** | One API over Windows Service / launchd / systemd; `vmrc-agent install` registers boot-start |
| Auth model | **Tailnet identity via tsnet `WhoIs`** + optional shared token | Upgrade over a shared-token-only scheme; authorize by Tailscale identity / ACL tag |
| Host UX | **CLI now, MCP server later** | `vmrc` CLI drives the fleet; MCP mode is a documented phase (P6) |
| Repo visibility | **MAY be public / MIT** | Original code with no redistribution constraint — so the repo must stay clean of any other project's name or personal directories |

## Source to lift from

The proven control agent's source location is recorded in the local, gitignored
`LIFT_SOURCE.local.md` (never committed). The four source files (server, tests, screen-capture,
entrypoint) are also captured in the brainstorming session transcript. Port them, don't rewrite:

- the 6-endpoint server (`authed()`, `safeJoin()`) — **core** → `internal/agent/server.go`
- its test file — 19 tests; regression baseline → `internal/agent/server_test.go`
- its screen-capture file — `ScreenCapturer` (generalize build tags) → `internal/agent/capture.go`
- its entrypoint pattern → `cmd/vmrc-agent/`

## What Claude Code must NOT do

- **Do not reference the origin project or personal directories** anywhere in committed files.
  The repo may be public; keep it clean. Source paths live only in the gitignored
  `LIFT_SOURCE.local.md`.
- **Do not commit secrets.** Tailscale auth keys and shared tokens are supplied at runtime
  (env / config file), never baked into the binary or committed. `.gitignore` covers
  `vmrc.local.*`, `*.local.*`, `.env`, and `*.state` / tsnet state dirs.
- **Do not bind the agent to a public address.** The tailnet (tsnet listener) is the only
  ingress. No `0.0.0.0` public listener.
- **Do not add dependencies beyond `tsnet`, `kardianos/service`, `kbinani/screenshot`,
  `urfave/cli/v3`, `golang.org/x/sys` (Windows elevation check; maintainer-approved P2),
  `github.com/minio/selfupdate` (atomic in-place binary replacement for self-update;
  maintainer-approved), and an MCP SDK (P6)** without asking the maintainer first.
- **Do not push to `main` directly.** Feature branches, intentional merges.
- **Do not copy any application-specific bits** (a hardcoded log allowlist, packaging logic) —
  this tool is generic; paths/allowlists are configurable.

## Conventions

- **Go**, modern toolchain, standard layout: `cmd/vmrc/`, `cmd/vmrc-agent/`, `internal/agent/`,
  `internal/host/`, `internal/service/`, `internal/tunnel/`, `internal/config/`. Table-driven
  tests; keep the lifted tests green.
- **Scripts:** kebab-case if any glue scripts are added.
- **Config:** flags + env + JSON file; document every key in `vmrc.example.json`.
- **Errors:** wrap with context; the agent logs every request (operator visibility is a feature).
- **Cross-compile:** `GOOS`/`GOARCH` matrix with `-ldflags="-s -w"` (see ARCHITECTURE → Build).

## When in doubt

- Prefer lifting proven code over rewriting.
- Prefer identity-based auth over tokens; prefer fewer config knobs.
- Two binaries by design (CLI + agent); one service per agent.
- Get the thing reachable end-to-end early (ROADMAP order), then harden.
