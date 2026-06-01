# CLAUDE.md — Project context for Claude Code

This file is read automatically by Claude Code when working in this repo. It captures
decisions already made so the agent doesn't re-litigate them.

## What this project is

`vmrc` is a standalone **VM remote-control** tool: a single Go binary that runs as a boot
service on a VM, joins your Tailscale tailnet by itself (embedded `tsnet`), and exposes a
small identity-gated HTTP API (screenshot / read / exec / spawn / get / put). The same binary
is the host-side CLI that drives a fleet of VMs over the tailnet. A later phase adds an MCP
server so AI agents drive VMs natively.

It is the generalization of the `inspect-agent` from Patrick's `REDACTED` project. See
[CONCEPT.md](./CONCEPT.md), [ARCHITECTURE.md](./ARCHITECTURE.md), [ROADMAP.md](./ROADMAP.md).

**Status:** pre-implementation. This repo currently holds only handoff docs. Start with
[ROADMAP.md](./ROADMAP.md) P0.

## Decisions already made (do not re-litigate)

| Topic | Choice | Why |
|---|---|---|
| Language | **Go** | Lifts the existing ~270-line inspect server almost verbatim; `tsnet` is Go-only |
| Tunnel | **Embed `tailscale.com/tsnet`** | The binary is its own tailnet node — no system Tailscale install on the VM; tunnel up on boot with the process |
| Target OS | **Windows, Linux, macOS** (not FreeBSD) | `kbinani/screenshot` + `kardianos/service` cover exactly these three |
| Auto-start | **`github.com/kardianos/service`** | One API over Windows Service / launchd / systemd; `vmrc install` registers boot-start |
| Auth model | **Tailnet identity via tsnet `WhoIs`** + optional shared token | Upgrade over the shared `m2debug`-style token; authorize by Tailscale identity / ACL tag |
| Host UX | **CLI now, MCP server later** | `vmrc` CLI drives the fleet; MCP mode is a documented phase-2 (P6) |
| Repo visibility | **MAY be public / MIT** | This is Patrick's own original code — unlike `REDACTED`, there is **no** local-only / never-push redistribution constraint here |

## Source to lift from (in REDACTED)

`/Users/REDACTED/dev/REDACTED/` — port these, don't rewrite:

- `patcher/internal/inspect/server.go` — the 6-endpoint server, `authed()`, `safeJoin()`. **Core.**
- `patcher/internal/inspect/server_test.go` — 13 tests; regression baseline.
- `patcher/internal/inspect/capture_windows.go` — `ScreenCapturer` (generalize build tags).
- `patcher/cmd/inspect-agent/main.go` — flag/entrypoint pattern.
- `patcher/build.sh` — cross-compile pattern.
- `.claude/skills/REDACTED/SKILL.md` — intended agent UX (model for the P6 MCP tools).

## What Claude Code must NOT do

- **Do not commit secrets.** Tailscale auth keys and shared tokens are supplied at runtime
  (env / config file), never baked into the binary or committed. Add `vmrc.local.*`, `.env`,
  and `*.state` / tsnet state dirs to `.gitignore`.
- **Do not bind the agent to a public address.** The tailnet (tsnet listener) is the only
  ingress. No `0.0.0.0` public listener.
- **Do not add dependencies beyond `tsnet`, `kardianos/service`, `kbinani/screenshot`, and an
  MCP SDK** without asking Patrick first.
- **Do not push to `main` directly.** Feature branches, intentional merges.
- **Do not copy the REDACTED bits** (the `syserr/syslog` allowlist, PackMaker logic) —
  this tool is generic; paths/allowlists are configurable.

## Conventions

- **Go**, modern toolchain, standard layout: `cmd/vmrc/`, `internal/agent/`, `internal/host/`,
  `internal/service/`. Table-driven tests; keep the lifted tests green.
- **TS files / scripts:** kebab-case if any glue scripts are added.
- **Config:** a `vmrc` config file + env overrides; document every key in a `vmrc.example.*`.
- **Errors:** wrap with context; the agent logs every request (operator visibility is a feature).
- **Cross-compile:** `GOOS`/`GOARCH` matrix with `-ldflags="-s -w"` (see ARCHITECTURE → Build).

## When in doubt

- Prefer lifting proven code over rewriting.
- Prefer identity-based auth over tokens; prefer fewer config knobs.
- Prefer one binary / one service over many.
- Get the thing reachable end-to-end early (ROADMAP order), then harden.
