# ARCHITECTURE — vmrc

This is the technical design the implementation session should build toward. It is a target,
not law — deviate where reality demands, but record why.

## Two binaries, one module

`vmrc` ships as **two cross-compiled binaries** built from one module, sharing logic through
`internal/*` packages:

**`vmrc-agent` (runs on the VM):**
- `vmrc-agent run` — run the agent in the foreground (used by the service supervisor).
- `vmrc-agent install` / `vmrc-agent uninstall` — register/remove the OS boot service.
- `vmrc-agent start` / `vmrc-agent stop` / `vmrc-agent status` — control the installed service.

**`vmrc` (the host CLI, runs on your main machine):**
- `vmrc ls` — list reachable vmrc VMs on the tailnet.
- `vmrc exec <vm> -- <cmd…>` — run a command on a VM, stream stdout + exit code.
- `vmrc shot <vm> [-o file.png]` — grab a screenshot.
- `vmrc get <vm>:<path> <local>` / `vmrc put <local> <vm>:<path>` — file transfer.
- `vmrc cp` — convenience wrapper over get/put.
- `vmrc mcp` — run an MCP server (stdio) exposing these verbs as tools (see
  [MCP server mode](#mcp-server-mode)).

Splitting the roles keeps each binary's responsibility (and build target) clear: build
`vmrc-agent` per VM OS/arch and copy it to a VM; build `vmrc` for your host. Shared code
(`internal/agent` types, `internal/config`) lives in the one module, so there is no duplication.

## The agent

### HTTP surface (lifted from the proven control agent, generalized)

Lift the control agent's `server.go` into `internal/agent/`. Endpoint contract:

| Endpoint | Method | Params | Returns | Notes |
|---|---|---|---|---|
| `/screenshot` | GET | — | PNG (`image/png`) | platform capturer; multi-display handling |
| `/read/` (was `/logs/`) | GET | path under allowlist/base | file bytes | **generalize**: configurable allow-globs, not a hardcoded log list |
| `/exec` | GET | `cmd` | stdout (`text/plain`) + `X-Exit-Status` header | synchronous; default 10-min timeout via `context.WithTimeout` |
| `/spawn` | GET | `cmd` | PID (`text/plain`) | detached, no timeout |
| `/get` | GET | `path` | file bytes | `safeJoin` traversal guard, rooted at base dir |
| `/put` | POST/PUT | `path`, body | confirmation | `safeJoin`; 405 on GET |

Status codes carried over: `400` missing param, `403` bad token / path escape, `405` wrong
method, `500` exec/io error. Keep `safeJoin()` and the per-request console logging verbatim —
operator visibility ("what is the remote driver doing") is a feature.

**Generalization tasks:** rename `/logs/` → `/read/` (or keep `/logs/` as an alias), replace
the hardcoded `allowedLogs` map with config-driven allow-globs, and make the working/base
directory a config value rather than a flag default.

### The tunnel: embedded tsnet

The agent does **not** call `http.ListenAndServe` on a plain TCP listener. It builds a tsnet
node and listens on the tailnet:

```go
srv := &tsnet.Server{
    Hostname:      cfg.Hostname,            // tailnet node name, e.g. "vmrc-buildbox"
    AuthKey:       cfg.AuthKey,             // or TS_AUTHKEY env; tagged, ideally ephemeral=false
    Dir:           cfg.StateDir,            // persisted node state (tailscaled.state)
    AdvertiseTags: []string{"tag:vmrc"},    // ACL handle for "this is a vmrc agent"
    // Ephemeral:  true,  // only if you want the node to vanish when the process stops
}
ln, err := srv.Listen("tcp", ":47800")      // returns a tailnet-bound net.Listener
lc, _ := srv.LocalClient()                  // for WhoIs identity checks (see Security)
http.Serve(ln, mux)
```

Key consequences:
- No system Tailscale needed on the VM — userspace WireGuard inside the process.
- The tunnel is up as soon as the service starts → satisfies "auto-tunnel on boot".
- Auth-key precedence: `Server.AuthKey` → `TS_AUTHKEY` → `TS_AUTH_KEY` → OAuth/WIF → interactive.
  Use a **tagged auth key** (so the node lands with `tag:vmrc`) supplied at runtime.
- State persists in `Dir`; after first enrollment the auth key is ignored unless
  `TSNET_FORCE_LOGIN=1`.

> Reference: <https://pkg.go.dev/tailscale.com/tsnet> — `Server` fields, `Listen`,
> `LocalClient()`, `WhoIs`.

### Service install: kardianos/service

Use `github.com/kardianos/service` for one API across Windows Service, macOS launchd, and
Linux systemd/openrc. `vmrc-agent install` registers boot-start; the service body runs
`vmrc-agent run`.

```go
svcConfig := &service.Config{
    Name:        "vmrc",
    DisplayName: "vmrc agent",
    Description: "Tailnet-gated VM remote control agent",
    Option: service.KeyValue{
        "StartType": "automatic",   // Windows: start at boot
        "OnFailure": "restart",     // Windows: auto-restart
        "RunAtLoad": true,          // launchd
        "KeepAlive": true,          // launchd: restart if it dies
    },
}
```

Per-OS notes for the implementation/runbook:
- **Windows:** registers a Windows Service (LocalSystem). Firewall is a non-issue for tsnet
  (userspace WG, outbound). Screenshot from a service session needs the right desktop/session
  context — verify capture works under the service account (may need session-1 interaction or
  running the agent in the user session for screenshots).
- **macOS:** LaunchDaemon for boot-start. Screenshots require **Screen Recording permission**
  granted to the binary — document the TCC prompt; a LaunchDaemon (root, no GUI session) may
  not be able to capture the user's screen, so a LaunchAgent in the user session may be needed
  for `shot`. Flag this as a known caveat to resolve in P2.
- **Linux:** systemd unit. Screenshots need an X11 display (`DISPLAY`/`XAUTHORITY`); Wayland is
  not supported by `kbinani/screenshot` — document that `shot` is best-effort on Linux and
  exec/get/put are the primary use there.

> Reference: <https://github.com/kardianos/service> — supports Windows / launchd / systemd /
> openrc, with install/uninstall/start/stop.

### Screenshot capture

Lift `capture_windows.go` (`ScreenCapturer` over `github.com/kbinani/screenshot`). That
library is cross-platform (Windows, macOS, Linux/X11), so generalize the build tags to cover
all three rather than `//go:build windows` only. Keep a `Capturer` interface so the HTTP layer
stays platform-agnostic and tests can inject a fake (as the existing tests do).

### Config

A small config file (e.g. `vmrc.yaml`/`vmrc.json`) + env overrides:

| Key | Meaning |
|---|---|
| `hostname` | tsnet node name |
| `authkey` | tailnet auth key (prefer `TS_AUTHKEY` env; never commit) |
| `state_dir` | tsnet state dir |
| `base_dir` | root for `exec` cwd, `get`/`put`, `read` |
| `read_globs` | allowlist of readable paths (replaces hardcoded log list) |
| `token` | optional shared-token fallback (prefer empty → identity-only) |
| `listen_port` | default `47800` |

## The host CLI

### VM discovery

The host already runs system Tailscale. Discover agents via the local tailnet:
- `tailscale status --json` and filter nodes carrying `tag:vmrc`, **or**
- a static config list of VM names/IPs for environments without tag access.

`vmrc ls` prints node name, tailnet IP/MagicDNS, online state.

### Command mapping

Each verb is a thin HTTP client to `https://<vm-magicdns-or-100.x>:47800/<endpoint>`
(HTTPS by default; `--scheme http` for agents on a tailnet without HTTPS certs):
- `vmrc exec <vm> -- tasklist` → GET `/exec?cmd=…`, print body, exit with `X-Exit-Status`.
- `vmrc shot <vm> -o s.png` → GET `/screenshot`, write PNG.
- `vmrc get <vm>:C:\out\a.epk ./a.epk` → GET `/get?path=…`.
- `vmrc put ./tool.exe <vm>:C:\tools\tool.exe` → PUT `/put?path=…` with body.

Targeting multiple VMs (`vmrc exec vm1,vm2 -- …` or `--tag vmrc`) fans out concurrently and
labels output per VM.

## Security model

Defense in depth, strongest-first:

1. **Tailnet membership** — the agent listens only on the tsnet listener; there is no
   public-internet path. Off-tailnet callers cannot reach it at all.
2. **Tailscale ACLs** — restrict which tailnet identities/tags may reach `tag:vmrc:47800`.
   This is the primary gate; configure it in the tailnet policy.
3. **Identity authorization (preferred)** — on each request, `lc.WhoIs(ctx, r.RemoteAddr)`
   yields the caller's Tailscale identity (`UserProfile.LoginName`, node tags). Authorize
   against an allowed-identities / allowed-tags list. This replaces the shared token as the
   default.
4. **Shared token (fallback)** — keep the constant-time token check for non-tsnet/local calls
   or environments where identity isn't available; empty token still means "reject all" to
   prevent accidental open access.
5. **Path-traversal guard** — keep `safeJoin()` for `get`/`put`/`read`; rooted at `base_dir`.
6. **Audit logging** — every request logged to the agent's stdout/service log.

**Never** bind the agent to a public address. **Never** commit auth keys or tokens.

## Build & cross-compile

Use a `GOOS`/`GOARCH` cross-compile script with `-ldflags="-s -w"`. Target matrix:

| OS | GOARCH | Notes |
|---|---|---|
| windows | amd64 | primary target |
| linux | amd64, arm64 | servers / build hosts |
| darwin | amd64, arm64 | Mac VMs/hosts |

Auth keys are **runtime** (env/config), not baked at build time — keeps binaries
distributable and secret-free. (Build-time bake is possible via `-X` like the patcher's
`defaultServer`, but rejected here because it embeds a secret.)

## MCP server mode

`vmrc mcp` runs an MCP server over **stdio** (the transport Claude Code / Claude Desktop
launch) built on the official Go SDK (`github.com/modelcontextprotocol/go-sdk`). It exposes
the host verbs as tools — `vmrc_ls`, `vmrc_exec`, `vmrc_screenshot`, `vmrc_click`,
`vmrc_type`, `vmrc_key`, `vmrc_get`, `vmrc_put` — so an AI agent drives the fleet natively
instead of shelling out to the CLI.

- **Layout:** tool logic lives in `internal/mcpserver`; `cmd/vmrc/mcp.go` only wires it to
  the stdio transport. Tools reuse `host.Discoverer` (target resolution) and `host.Client`
  (the HTTP verbs) through an injectable `Config`, the same `clientFor` the CLI uses, so the
  global flags (`--token`, `--port`, `--scheme`, `--tag`, `--timeout`) apply unchanged. Tests
  drive the server end-to-end over the SDK's in-memory transport against an `httptest` fake
  agent.
- **Typed inputs:** each tool has a Go input struct whose `jsonschema` tags become the
  advertised JSON Schema; the SDK validates arguments before the handler runs.
- **Results:** `vmrc_exec` returns combined output plus exit code (non-zero exit is a normal
  result; output beyond 1 MiB is truncated); `vmrc_screenshot` returns PNG image content;
  `vmrc_get` returns UTF-8 text ≤ 1 MiB inline, or writes atomically to `local_path`
  (`internal/atomicfile`, shared with `vmrc get`).
- **Errors:** discovery and agent failures (unknown VM, 403, unreachable, timeout) become
  `isError` tool results with the agent's message, never protocol errors or a crash.
- **Not exposed:** self-update (`/update`). Replacing agent binaries is an operator
  decision, not something an autonomous model should trigger.
- **stdout discipline:** stdout is the JSON-RPC channel; the server logs every tool call to
  stderr (typed text is logged by length only).
