# ROADMAP — vmrc

Phased plan for the implementation session. Each phase has a concrete done-check. Phases are
ordered so the thing works end-to-end as early as possible (skeleton → tunnel → service →
auth → fleet → polish), then MCP last.

---

## P0 — Skeleton + lifted core

Stand up the Go module and the HTTP server on a plain TCP listener (no tunnel yet) so the
proven core runs and its tests pass.

- `go mod init`; pick a module path (e.g. `github.com/pddstudio/vmrc`).
- Command structure: `vmrc-agent run` (the only working subcommand this phase).
- **Lift** the control agent's `server.go` → `internal/agent/server.go`.
- **Lift** its `server_test.go` → keep all 19 tests green.
- **Lift** the entrypoint pattern into `cmd/vmrc-agent/`; listen on plain `:47800` for now.
- Keep `safeJoin`, `authed`, per-request logging.

**Done when:** `go test ./...` is green and `vmrc-agent run` serves `/exec`, `/get`, `/put`,
`/spawn` against `curl` on localhost.

---

## P1 — Embedded tunnel (tsnet)

Replace the plain listener with a tsnet node so the binary joins the tailnet itself.

- Add `tailscale.com/tsnet`.
- Build `tsnet.Server{Hostname, AuthKey, Dir, AdvertiseTags:["tag:vmrc"]}`; `srv.Listen("tcp", ":47800")`.
- Auth key from `TS_AUTHKEY` env / config; persist state in `Dir`.
- Keep a `--local` dev flag to fall back to plain TCP for fast iteration.

**Done when:** with a tagged auth key set, `vmrc-agent run` appears in the tailnet as
`vmrc-…` and is reachable from the host at `http://<magicdns>:47800/exec?cmd=…`. No system
Tailscale on the agent box.

> Ref: <https://pkg.go.dev/tailscale.com/tsnet>

---

## P2 — Boot service (kardianos/service)

Make it install itself and run on boot.

- Add `github.com/kardianos/service`.
- `vmrc-agent install|uninstall|start|stop|status`; service body runs the agent.
- `StartType: automatic`, `OnFailure/KeepAlive: restart`.
- Resolve the **screenshot-under-service** caveats per OS (Windows session, macOS Screen
  Recording / LaunchAgent-vs-Daemon, Linux X11/Wayland) — document outcomes in a runbook.

**Done when:** after `vmrc-agent install` + reboot, the agent is up and on the tailnet with no manual
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

Strip any application-specific assumptions; ship artifacts.

- Replace hardcoded log allowlist with config-driven `read_globs`; `/logs/` → `/read/` (alias old).
- Config file + env overrides (see ARCHITECTURE → Config).
- **Lift** the control agent's screen-capture file; generalize build tags to win/darwin/linux.
- Cross-compile matrix (win/linux/darwin × amd64/arm64), `-ldflags="-s -w"`; a `build.sh`/Makefile.
- README quick-start verified on a clean clone.

**Done when:** release binaries exist for all target OS/arch, no application-specific code
remains, and a fresh clone builds and runs per the README.

---

## P6 — MCP server ✅ done

- `vmrc mcp` — stdio MCP server (official Go SDK) exposing `vmrc_ls`, `vmrc_exec`,
  `vmrc_screenshot`, `vmrc_click`, `vmrc_type`, `vmrc_key`, `vmrc_get`, `vmrc_put`, reusing the
  P4 host client (`internal/mcpserver`).
- Tool UX modelled on the `vmrc-cli` skill: `vmrc_ls` → screenshot → act → screenshot.
- Self-update is intentionally **not** exposed as a tool.

**Done when:** an AI agent can drive a VM through the MCP tools without curl/CLI.

---

## Hardening: done

- **Signed self-update (from the 2026-06-04 audit).** Before this, `selfupdate.Apply` checked the
  archive's SHA-256 against `checksums.txt` from the *same* release. That catches transport
  corruption, but not a compromised GitHub release, token, or maintainer account that publishes a
  malicious archive with a matching checksum. Now:
  - The release pipeline signs `checksums.txt` with ed25519 (stdlib `crypto/ed25519`, no new
    dependency) and publishes `checksums.txt.sig`. goreleaser's `signs:` step runs
    `go run ./tools/releasesign sign`. The private key exists only as the
    `VMRC_RELEASE_SIGNING_KEY` CI secret. `release.yml` stops before tagging when that secret is
    missing or doesn't match a committed public key.
  - Both binaries embed the trusted public keys from `internal/selfupdate/keys/*.pub`. Before any
    checksum is used, `Apply` verifies the signature over the exact `checksums.txt` bytes. All
    three update paths go through `Apply`: `vmrc update --self`, `vmrc-agent update` and the
    agent's `/update`. Verification fails closed (`selfupdate.ErrSignature`) in four cases: the
    `.sig` asset is missing, the signature is malformed, the signing key isn't trusted, or the
    build has no key embedded. There is no bypass flag.
  - You can rotate keys: every `.pub` file is trusted, and the signature names its key id. The
    maintainer steps are in CONTRIBUTING.md, section "Release signing key".

## Backlog: hardening follow-ups

- **Install scripts don't check the signature yet.** `scripts/install.sh` and `install.ps1` only
  check SHA-256 against `checksums.txt`, so the *first* install still trusts the GitHub release.
  Every self-update after that is signature-verified. Closing this gap means verifying ed25519 in
  POSIX shell and PowerShell (or shipping a small verifier), and that tooling choice needs the
  maintainer's decision. Until then, `go run ./tools/releasesign verify -in checksums.txt -sig
  checksums.txt.sig` lets you check a release by hand from a clone.

---

## Dependencies summary

| Dep | Purpose | Phase |
|---|---|---|
| `github.com/urfave/cli/v3` | CLI subcommands + flag/env/JSON sources (both binaries) | P0 |
| `tailscale.com/tsnet` | embedded tailnet node | P1 |
| `github.com/kardianos/service` | boot service across OSes | P2 |
| `github.com/kbinani/screenshot` | desktop capture (win/mac/linux-X11) | P0/P5 |
| `github.com/modelcontextprotocol/go-sdk` | MCP server mode (`vmrc mcp`) | P6 |

Ask the maintainer before adding deps beyond these.
