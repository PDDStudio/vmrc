# ROADMAP — vmrc

Phased plan for the implementation session. Each phase has a concrete done-check. Phases are
ordered so the thing works end-to-end as early as possible (skeleton → tunnel → service →
auth → fleet → polish), then MCP last.

All "lift from" paths are in the `REDACTED` repo
(`/Users/REDACTED/dev/REDACTED/`). Port them; don't rewrite from scratch.

---

## P0 — Skeleton + lifted core

Stand up the Go module and the HTTP server on a plain TCP listener (no tunnel yet) so the
proven core runs and its tests pass.

- `go mod init`; pick a module path (e.g. `github.com/pddstudio/vmrc`).
- Command structure: `vmrc agent` (the only working subcommand this phase).
- **Lift** `patcher/internal/inspect/server.go` → `internal/agent/server.go`.
- **Lift** `patcher/internal/inspect/server_test.go` → keep all 13 tests green.
- **Lift** `patcher/cmd/inspect-agent/main.go` flag handling; listen on plain `:47800` for now.
- Keep `safeJoin`, `authed`, per-request logging.

**Done when:** `go test ./...` is green and `vmrc agent` serves `/exec`, `/get`, `/put`,
`/spawn` against `curl` on localhost.

---

## P1 — Embedded tunnel (tsnet)

Replace the plain listener with a tsnet node so the binary joins the tailnet itself.

- Add `tailscale.com/tsnet`.
- Build `tsnet.Server{Hostname, AuthKey, Dir, AdvertiseTags:["tag:vmrc"]}`; `srv.Listen("tcp", ":47800")`.
- Auth key from `TS_AUTHKEY` env / config; persist state in `Dir`.
- Keep a `--local` dev flag to fall back to plain TCP for fast iteration.

**Done when:** with a tagged auth key set, `vmrc agent` appears in the tailnet as
`vmrc-…` and is reachable from the host at `http://<magicdns>:47800/exec?cmd=…`. No system
Tailscale on the agent box.

> Ref: <https://pkg.go.dev/tailscale.com/tsnet>

---

## P2 — Boot service (kardianos/service)

Make it install itself and run on boot.

- Add `github.com/kardianos/service`.
- `vmrc install|uninstall|start|stop|status`; service body runs the agent.
- `StartType: automatic`, `OnFailure/KeepAlive: restart`.
- Resolve the **screenshot-under-service** caveats per OS (Windows session, macOS Screen
  Recording / LaunchAgent-vs-Daemon, Linux X11/Wayland) — document outcomes in a runbook.

**Done when:** after `vmrc install` + reboot, the agent is up and on the tailnet with no manual
step, on at least Windows and Linux (macOS screenshot caveat documented if unresolved).

> Ref: <https://github.com/kardianos/service>

---

## P3 — Identity-based auth

Upgrade from shared-token to Tailscale identity.

- Use `srv.LocalClient().WhoIs(ctx, r.RemoteAddr)` per request.
- Authorize against allowed login(s)/tag(s) from config; token becomes optional fallback.
- Document the recommended tailnet ACL (only your host identity/tag may reach `tag:vmrc:47800`).

**Done when:** a caller whose Tailscale identity isn't allowlisted gets `403`, even with the
right token; an allowed identity needs no token.

---

## P4 — Host CLI (fleet)

The controller side.

- `vmrc ls` — enumerate `tag:vmrc` nodes via local `tailscale status --json` (or config list).
- `vmrc exec/shot/get/put/cp <vm…>` — HTTP client to the agent endpoints; propagate exit codes.
- Multi-target fan-out with per-VM labeled output.

**Done when:** from the host, `vmrc ls` lists the VMs and `vmrc exec <vm> -- <cmd>` /
`vmrc shot <vm>` work against a real agent over the tailnet.

---

## P5 — Generalize + cross-build release

Strip REDACTED specifics; ship artifacts.

- Replace hardcoded log allowlist with config-driven `read_globs`; `/logs/` → `/read/` (alias old).
- Config file + env overrides (see ARCHITECTURE → Config).
- **Lift** `patcher/internal/inspect/capture_windows.go`; generalize build tags to win/darwin/linux.
- Cross-compile matrix (win/linux/darwin × amd64/arm64), `-ldflags="-s -w"`; a `build.sh`/Makefile.
- README quick-start verified on a clean clone.

**Done when:** release binaries exist for all target OS/arch, no game-specific code remains,
and a fresh clone builds and runs per the README.

---

## P6 — MCP server (later)

- `vmrc mcp` — expose verbs as MCP tools (`vmrc_exec`, `vmrc_screenshot`, `vmrc_get`,
  `vmrc_put`, `vmrc_ls`), reusing the P4 host client.
- Model the tool UX on `REDACTED/.claude/skills/REDACTED/SKILL.md`.

**Done when:** an AI agent can drive a VM through the MCP tools without curl/CLI.

---

## Dependencies summary

| Dep | Purpose | Phase |
|---|---|---|
| `tailscale.com/tsnet` | embedded tailnet node | P1 |
| `github.com/kardianos/service` | boot service across OSes | P2 |
| `github.com/kbinani/screenshot` | desktop capture (win/mac/linux-X11) | P0/P5 |
| an MCP Go SDK (TBD) | MCP server mode | P6 |

Ask Patrick before adding deps beyond these.
