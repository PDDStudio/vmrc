# vmrc — VM remote control

[![CI](https://github.com/pddstudio/vmrc/actions/workflows/ci.yml/badge.svg)](https://github.com/pddstudio/vmrc/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/pddstudio/vmrc?sort=semver)](https://github.com/pddstudio/vmrc/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/pddstudio/vmrc)](./go.mod)

`vmrc` is a small tool for **remote-controlling a fleet of virtual machines over a
Tailscale tailnet** — no RDP, no SSH, no system Tailscale install required on the VM.
From your machine you can screenshot a VM's desktop, click and type into its GUI, run
shell commands, and move files in and out — across one VM or many at once.

It ships as **two binaries** built from one Go module:

- **`vmrc`** — the host-side CLI you run from your own machine to drive the fleet
  (`vmrc ls`, `vmrc exec`, `vmrc shot`, `vmrc click`/`type`/`key`, `vmrc get`/`put`).
- **`vmrc-agent`** — a single binary you drop onto a VM. It installs itself as a
  boot/login service, joins your tailnet on its own (embedded `tsnet`), and serves a
  small, identity-gated HTTP API the CLI talks to.

Because the interface is just a CLI, it's equally drivable by a **human or an AI agent**.
A bundled Claude Code skill teaches an agent the *screenshot → reason → act* loop
([install it below](#3-install-the-ai-agent-skill)), and `vmrc mcp` exposes the same verbs
as native **MCP tools** for Claude Code, Claude Desktop, or any MCP client
([see below](#use-with-claude-code--mcp-clients)).

## How it works

```
  your machine                                VM 1 (Windows)
  ┌─────────────────┐     tailnet (WG)     ┌───────────────────────────┐
  │ vmrc ls/exec/…  │◀───────────────────▶ │ vmrc-agent (boot service) │
  │ (you / AI agent)│  https://vm:47800    │   embeds tsnet  ──► tailnet│
  └─────────────────┘                      │   serves /exec /shot /…    │
          ▲                                └───────────────────────────┘
          │  same tailnet                   VM 2 (Linux), VM 3 (macOS) …
          └──────────────────────────────────────────────────────────────
```

The **trust boundary is the tailnet plus authentication — nothing else.** Tailscale
ACLs decide who can reach an agent; the agent then authorizes callers by their
Tailscale identity (and, optionally, a shared token). The agent never binds to a
public address. See [Restricting who can drive an agent](#restricting-who-can-drive-an-agent).

Traffic is **HTTPS by default**: when your tailnet has HTTPS certificates enabled
(MagicDNS, on by default for modern tailnets) the agent serves TLS automatically and
the CLI talks HTTPS. On a tailnet without certificates the agent falls back to plain
HTTP over the WireGuard-encrypted tailnet; reach those agents with `--scheme http`.

---

## 1. Install the host CLI (`vmrc`)

Run this on **your own machine** — the one that already has Tailscale running.

**Quick install** (downloads the latest release for your OS/arch, SHA-256-verifies it
against the release `checksums.txt`, and puts it on your `PATH`):

```sh
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/pddstudio/vmrc/main/scripts/install.sh | sh
```

The installer honors `VMRC_VERSION` (pin a tag) and `VMRC_BIN_DIR` (install location).

**From source:**

```sh
go build -o vmrc ./cmd/vmrc      # or: make build  (both binaries → bin/)
vmrc --version                   # reports the build stamp
```

Verify it can see your fleet:

```sh
vmrc ls                          # lists tag:vmrc agents on your tailnet
```

## 2. Install the agent on a VM (`vmrc-agent`)

Run this **on each VM you want to control**, from a privileged shell.

First, create a **tagged auth key** (`tag:vmrc`) in the
[Tailscale admin console](https://login.tailscale.com/admin/settings/keys).

**Quick install:**

```powershell
# Windows VM — from an Administrator PowerShell:
irm https://raw.githubusercontent.com/pddstudio/vmrc/main/scripts/install.ps1 | iex
```

```sh
# Linux / macOS VM — installs vmrc-agent, then prints the setup step:
curl -fsSL https://raw.githubusercontent.com/pddstudio/vmrc/main/scripts/install.sh | VMRC_BINARY=vmrc-agent sh
```

> These one-liners fetch the install script from `main`. To pin it, replace `main` in
> the URL with a release tag (e.g. `v0.3.0`). Every release archive carries SLSA build
> provenance; check one with `gh attestation verify <archive> -R pddstudio/vmrc`.

Then enroll the VM by running `setup` with the auth key in the environment, and say
who may drive it (your Tailscale login):

```sh
TS_AUTHKEY=tskey-... vmrc-agent setup --allow-login you@example.com
```

`setup` refuses to finish without `--allow-login`, `--allow-tag`, or `--token-file`:
an agent with none of them would deny every request.

That's it. `setup` writes the config, copies the agent into a managed working
directory, enrolls the VM on your tailnet, and installs + starts the service so it
comes back automatically after a reboot or logon. The auth key is used only during
enrollment and is never written to disk. Re-running `setup` is safe.

The VM now appears in `vmrc ls` on your host, ready to drive.

### Install modes (user vs. system)

`setup` (and the lower-level `install`/`start`/`stop`/`status`/`uninstall`) default to
**user-session mode** and accept a `--system` override:

| | Default — user mode | `--system` |
|---|---|---|
| Runs as | the logged-in user, in their desktop session | LocalSystem (boot service) |
| Autostart | logon (Windows Scheduled Task; macOS/Linux user agent) | boot, before login |
| Working dir | `%LOCALAPPDATA%\vmrc` (user-owned) | `C:\ProgramData\vmrc` (machine-wide) |
| Screen capture | ✅ captures the live desktop | ❌ session-0 isolated |
| `exec` privilege | the user, elevated (no UAC) | `SYSTEM` |
| Available before login / at lock screen | ❌ (pair with auto-logon for headless) | ✅ |

User mode is the default because it can see and drive the desktop. The agent's binary
lives under `<workdir>/bin/` and `update` replaces it there (no elevation needed in
user mode).

See [docs/runbook-windows.md](docs/runbook-windows.md) for the lower-level commands and
where state lives.

> **macOS screenshots:** release `vmrc-agent` binaries are built with `CGO_ENABLED=0`,
> so on macOS `/screenshot` fails with HTTP 500 `no active displays` (`exec`/`get`/`put`/`read`
> work normally). Screen capture on macOS needs a cgo build with the macOS SDK — build
> `vmrc-agent` locally on a Mac (`go build ./cmd/vmrc-agent`) if you need it. macOS capture
> also requires Screen-Recording (TCC) permission and a user-session service — see the runbook.

## 3. Install the AI-agent skill

`vmrc` is designed to be driven by an AI agent, not just a human. The repo ships a
**Claude Code skill** at [`.claude/skills/vmrc-cli/`](./.claude/skills/vmrc-cli/SKILL.md)
that teaches the agent the *screenshot → reason → act* loop over the CLI.

- **In this repo:** nothing to do — Claude Code auto-discovers the skill whenever you
  open the project.
- **From another workspace:** copy the skill into a directory the agent reads, then it
  becomes available there too:

  ```sh
  # project-level (this workspace only):
  cp -r .claude/skills/vmrc-cli /path/to/other-project/.claude/skills/

  # user-level (every workspace for your user):
  cp -r .claude/skills/vmrc-cli ~/.claude/skills/
  ```

Once installed, ask the agent to control a VM in plain language ("take a screenshot of
`buildbox`", "click the OK button", "run `systeminfo` on `vm1`") and it will drive the
CLI for you. The agent operates the `vmrc` CLI only — it never calls the HTTP API
directly, so the same tailnet + identity auth applies.

## Use with Claude Code / MCP clients

`vmrc mcp` runs an [MCP](https://modelcontextprotocol.io) server over stdio, so an AI agent
can drive the fleet through native tools instead of shelling out to the CLI. It uses the
same discovery, auth, and flags as the CLI (`--token`/`VMRC_TOKEN`, `--port`, `--scheme`,
`--tag`, `--timeout`); put global flags **before** `mcp`.

Register it with Claude Code:

```sh
claude mcp add vmrc -- vmrc mcp
# agents on a tailnet without HTTPS certs, user-wide, with a token-fallback agent:
claude mcp add --scope user -e VMRC_TOKEN=… vmrc -- vmrc --scheme http mcp
```

Or add it to an MCP client's JSON config (Claude Desktop's `claude_desktop_config.json`,
a project `.mcp.json`, …):

```json
{
  "mcpServers": {
    "vmrc": {
      "command": "vmrc",
      "args": ["mcp"]
    }
  }
}
```

| Tool | Arguments | Returns |
|---|---|---|
| `vmrc_ls` | — | agents: name, DNS name, OS, online |
| `vmrc_exec` | `vm`, `command`, `timeout_seconds?` | combined stdout+stderr and exit code (non-zero exit is a normal result) |
| `vmrc_screenshot` | `vm` | the screen as a PNG image the model can see |
| `vmrc_click` | `vm`, `x`, `y`, `button?` (left/right/middle), `double?` | confirmation |
| `vmrc_type` | `vm`, `text` | confirmation (does not press Enter) |
| `vmrc_key` | `vm`, `combo` (e.g. `Enter`, `Ctrl+C`) | confirmation |
| `vmrc_get` | `vm`, `remote_path`, `local_path?` | with `local_path`: writes the file atomically on the host; without: the content as text (UTF-8, ≤ 1 MiB) |
| `vmrc_put` | `vm`, `remote_path`, and exactly one of `local_path` / `content` | bytes uploaded |

Agent-side failures (unknown VM, `403`, unreachable) come back as tool errors the model can
read and act on; they never take the server down. Self-update is **deliberately not exposed**
as a tool — replacing agent binaries stays an operator action (`vmrc update`). The MCP server
logs every tool call to stderr; stdout carries only the protocol.

> [!CAUTION]
> An MCP client with these tools has the same power as you at the CLI: arbitrary command
> execution and GUI control on every agent your tailnet identity can reach. Review tool calls
> (keep your client's per-tool approval on) and scope `--tag` to the VMs the agent should touch.

---

## Driving the fleet

```sh
vmrc ls                                  # list tag:vmrc agents on your tailnet
vmrc exec buildbox -- systeminfo         # run a command, stream output + exit code
vmrc exec vm1,vm2 -- whoami              # fan out to several VMs (labeled output)
vmrc exec --all -- "uptime"              # every tag:vmrc agent
vmrc shot buildbox -o shot.png           # screenshot the desktop
vmrc click buildbox 640 300              # click at absolute screen pixels
vmrc type  buildbox "hello world"        # type literal text
vmrc key   buildbox "Enter"              # press a key / combo (Tab, Ctrl+C, Alt+F4…)
vmrc get buildbox:out/app.zip ./app.zip   # paths are relative to the agent's base_dir
vmrc put ./tool.exe buildbox:tools/tool.exe   # lands in the agent's uploads dir
vmrc exec buildbox -- uploads\tools\tool.exe --version
```

`put` writes into the agent's **uploads dir** (`uploads_dir`, default
`<base_dir>/uploads`), not the base dir: `exec`/`spawn` run in `base_dir`, so they see
an upload at `uploads/<path>`, and `vmrc get buildbox:uploads/<path>` fetches it back.
Uploads older than `upload_retention` (default 7 days) are deleted automatically.

Discovery uses your system Tailscale (there's no agent list to maintain). Auth is by
your tailnet identity — an agent that allowlists you needs no token; for token-fallback
agents pass `--token` or set `VMRC_TOKEN`. The CLI talks **HTTPS by default**; pass
`--scheme http` for agents on a tailnet without HTTPS certs, and `--tag` to target a
different ACL tag.

> **Note:** `click`/`type`/`key` are Windows-only today (macOS/Linux return "not
> supported") and need a logged-in interactive session, the same as `shot`.

### Restricting who can drive an agent

> [!IMPORTANT]
> **The security boundary is the tailnet plus auth — nothing else.** The agent
> exposes `exec`/`spawn`, file `get`/`put`, and synthetic input by design, so
> **anyone authorized to reach an agent can run arbitrary code as the user the
> agent runs as.** There is no command allowlist or sandbox. Treat authorization
> to an agent as equivalent to a shell on that VM: keep agents off public
> interfaces (they bind only to the Tailscale listener), scope your Tailscale
> ACLs tightly, and use an identity allowlist (below) rather than a shared token
> wherever you can.

By default the agent is reachable by anything your Tailscale ACLs let onto port
`47800`. To authorize by **Tailscale identity**, set an allowlist — `allowed_logins`
(login names) and/or `allowed_tags` (ACL tags) in the config file, or
`--allow-login` / `--allow-tag` on `setup`/`run` (repeatable). With an allowlist:

- an allowlisted caller is authorized with **no token**;
- a caller whose identity resolves but is **not** allowlisted is denied **even with a
  valid token**;
- the optional shared token (`VMRC_TOKEN`, env only) is a fallback used **only** for
  callers whose identity can't be resolved.

With no allowlist, the agent falls back to shared-token auth; with neither, every
request is denied. A service gets its token from `token_file` (an owner-only file
named in the config); `VMRC_TOKEN` works for a foreground `run`. The `--local` dev
listener has no tailnet identity, so it is always token-only.

Don't put `tag:vmrc` (the tag agents carry) in `allowed_tags`: every agent could then
drive every other one. The agent warns at startup if you do.

**Browsers can't drive an agent.** Every request must carry the `X-Vmrc-Client`
header the CLI sends, and requests with browser `Origin`/`Sec-Fetch-*` headers are
refused. Without this, a web page open on an allowlisted machine could reach the
agent over the tailnet and, needing no token, run commands. One consequence: an agent
from v0.3.0 on requires a CLI from v0.3.0 or newer (older CLIs get 403/405).

## Updating

Both binaries update themselves from this repo's GitHub Releases. Each update goes
through these steps:

1. Download the release's `checksums.txt` and its signature, `checksums.txt.sig`.
2. Verify the ed25519 signature against the release public key compiled into the
   binary.
3. SHA-256-verify the downloaded archive against the now-authenticated
   `checksums.txt`.
4. Atomically replace the binary's own executable.

An update is refused, and the installed binary left untouched, when the release is
unsigned, the signature is invalid, or the signing key is unknown. A build without an
embedded release key can't self-update at all. There is no flag to skip this check.
See [SECURITY.md](./SECURITY.md#release-signing).

No GitHub token is needed. Set `GITHUB_TOKEN` only if you hit the unauthenticated
API rate limit.

```sh
vmrc update --self                   # update this local vmrc CLI
vmrc update --self --check           # report the available version, install nothing
vmrc update --self --version v0.3.0  # pin a specific release (reinstall/downgrade)

vmrc update --all                    # drive every tag:vmrc agent to self-update
vmrc update buildbox,vm1             # update named agents
vmrc update --all --check            # report each agent's available version
vmrc update buildbox --version v0.3.0 --downgrade   # roll an agent back to an older release
```

On a VM you can also update the agent locally:

```sh
vmrc-agent update                    # stop → replace managed binary in bin/ → restart
vmrc-agent update --check            # report only
```

Remote agent updates (`vmrc update --all`) drive each agent's authenticated `/update`
endpoint: the agent fetches the release itself, replaces its binary, and exits so its
service relaunches the new binary. Moving an agent to an *older* release needs
`--downgrade` as well as `--version`. A dev/source build (`dev`, a `git describe` version, or a `go install`
pseudo-version) refuses to
update unless you pass `--force` or `--version <tag>`; prereleases are ignored unless
you pass `--pre`.

## Agent configuration

`vmrc-agent` reads config with precedence **flag → env → JSON file → default**.
See [`vmrc.example.json`](./vmrc.example.json) for a template. Keys with an env override
also take a kebab-case flag on `run`/`setup` (e.g. `--upload-retention 24h`).

| Key | Env override | Meaning |
|---|---|---|
| `hostname` | `VMRC_HOSTNAME` | tsnet node name on the tailnet; if empty/omitted, defaults to a DNS-sanitized form of the machine's OS hostname (falling back to `vmrc-agent`) |
| `state_dir` | `VMRC_STATE_DIR` | tsnet state dir (must be absolute) |
| `base_dir` | `VMRC_BASE_DIR` | root for `exec` cwd, `get`, `read` (must be absolute) |
| `uploads_dir` | `VMRC_UPLOADS_DIR` | where `put` writes (must be absolute; default `<base_dir>/uploads`, created `0750` at startup) |
| `upload_retention` | `VMRC_UPLOAD_RETENTION` | delete uploads whose mtime is older than this Go duration (default `168h`; `0` disables) |
| `retention_sweep_interval` | `VMRC_RETENTION_SWEEP_INTERVAL` | how often the retention sweep runs; it also runs once at startup (default `1h`) |
| `log_max_size_mb` | `VMRC_LOG_MAX_SIZE_MB` | rotate `agent.log` once it would exceed this many MB (default `10`; `0` disables) |
| `log_max_backups` | `VMRC_LOG_MAX_BACKUPS` | rotated logs kept as `agent.log.1` … `.N` (default `3`) |
| `bin_dir` | — | dir holding the managed agent binary for self-update; defaults to a `bin/` beside the config/base dir |
| `read_globs` | — | path globs under `base_dir` the `/read` endpoint may serve (`*` never crosses `/`). `get` is not limited by it: anyone authorized can read anything under `base_dir` |
| `listen_port` | `VMRC_LISTEN_PORT` | HTTP listen port (default `47800`) |
| `allowed_logins` | — | identity allowlist: Tailscale login names |
| `allowed_tags` | — | identity allowlist: ACL tags, e.g. `tag:vmrc-admin` |
| `token_file` | `VMRC_TOKEN_FILE` | file holding the shared token (must be `chmod 600` on Unix); how the installed service gets a token |

Unknown keys in the config file are an error (so a typo can't silently disable your
allowlist).

The retention sweep only removes regular files and symlinks under `uploads_dir` (judged
by their own mtime — symlinks are never followed) and then prunes directories it left
empty; it never deletes `uploads_dir` itself. Each sweep that removes something logs a
one-line summary.

**Secrets are never read from or written to the JSON file** (a secret key in it is an
error); they come from the environment, or for the token, from `token_file`:

| Secret | Env var |
|---|---|
| tailnet auth key (enrollment) | `TS_AUTHKEY` |
| shared-token fallback | `VMRC_TOKEN` (or `token_file`) |

The embedded Tailscale node doesn't upload its diagnostic logs to Tailscale unless
you set `VMRC_TAILSCALE_LOGS=1` (useful when working with Tailscale support).

## Building from source

```sh
make build         # both binaries → bin/ (version-stamped from git)
make dev-install   # build the CLI and symlink it onto your PATH (live rebuilds)
make check         # vet + test + lint (the CI gate); make help lists all targets
```

Plain `go`:

```sh
go build -o vmrc       ./cmd/vmrc          # host CLI
go build -o vmrc-agent ./cmd/vmrc-agent    # VM agent
```

All release targets at once (requires [GoReleaser](https://goreleaser.com)):

```sh
make snapshot        # win/linux/darwin × amd64/arm64 → dist/
make snapshot-host   # just your host
```

## Documentation

| File | What's in it |
|---|---|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Technical design: agent, tunnel, service, host CLI, endpoints, security |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | How to build, test, lint, and contribute |
| [.claude/skills/vmrc-cli/SKILL.md](./.claude/skills/vmrc-cli/SKILL.md) | The Claude Code skill: drive a VM fleet via the CLI |
| [README → MCP](#use-with-claude-code--mcp-clients) | `vmrc mcp`: the fleet verbs as MCP tools |
| [docs/testing-on-a-vm.md](./docs/testing-on-a-vm.md) | End-to-end Windows VM test walkthrough |
| [docs/runbook-windows.md](./docs/runbook-windows.md) | Windows install/enroll/operate runbook |

## License

[MIT](./LICENSE).
