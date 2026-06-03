# vmrc — VM remote control

[![CI](https://github.com/pddstudio/vmrc/actions/workflows/ci.yml/badge.svg)](https://github.com/pddstudio/vmrc/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/pddstudio/vmrc)](./go.mod)

> **Status: working.** Host CLI + self-installing agent + embedded tailnet + identity
> auth + cross-platform release builds are all implemented. On Windows the agent
> captures the logged-in user's desktop even when running as a boot service. The native
> MCP server mode (`vmrc mcp`) is planned. See the quick starts below.

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

## Quick start (agent)

On the VM you want to control, from a privileged shell (Administrator on Windows,
`sudo` on Linux):

1. Create a **tagged auth key** (`tag:vmrc`) in the Tailscale admin console.
2. Run setup with the key in the environment:

   ```sh
   TS_AUTHKEY=tskey-... vmrc-agent setup
   ```

That's it — `setup` writes the config, copies the agent into a managed working
directory, creates its directories, enrolls the VM on your tailnet, and installs +
starts the agent so it returns automatically after a reboot/logon. The key is used
only during enrollment and is never written to disk. Re-running `setup` is safe.

See [docs/runbook-windows.md](docs/runbook-windows.md) for the lower-level
commands (`run`/`install`/`start`/`stop`/`status`/`uninstall`) and where state lives.

### Install modes (user vs system)

`setup` (and `install`/`start`/`stop`/`status`/`uninstall`) default to **user-session
mode** and accept a `--system` override:

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
user mode). The `--system` service path is retained as an override and is being
hardened in a later phase.

## Quick start (host)

On your main machine (which already runs Tailscale), build `vmrc` and drive the fleet:

```sh
go build -o vmrc ./cmd/vmrc

vmrc ls                                  # list tag:vmrc agents on your tailnet
vmrc exec buildbox -- systeminfo         # run a command, stream output + exit code
vmrc exec vm1,vm2 -- whoami              # fan out to several VMs (labeled output)
vmrc exec --all -- "uptime"              # every tag:vmrc agent
vmrc shot buildbox -o shot.png           # screenshot
vmrc get buildbox:C:\out\app.zip ./app.zip
vmrc put ./tool.exe buildbox:C:\tools\tool.exe
```

Discovery uses your system Tailscale (no agent list to maintain). Auth is by your tailnet
identity — an agent that allowlists you needs no token; for token-fallback agents pass
`--token` or set `VMRC_TOKEN`. Use `--scheme https` if an agent serves TLS, and `--tag` to
target a different ACL tag.

### Restricting who can drive the agent

By default the agent is reachable by anything your Tailscale ACLs let onto port
`47800`. To authorize by **Tailscale identity**, set an allowlist — `allowed_logins`
(login names) and/or `allowed_tags` (ACL tags) in the config file, or
`--allow-login` / `--allow-tag` on `setup`/`run` (repeatable). With an allowlist:

- an allowlisted caller is authorized with **no token**;
- a caller whose identity resolves but is **not** allowlisted is denied **even with a
  valid token**;
- the optional shared token (`VMRC_TOKEN`, env only) is a fallback used **only** for
  callers whose identity can't be resolved.

With no allowlist, the agent falls back to shared-token auth (`VMRC_TOKEN`); with
neither, every request is denied. The `--local` dev listener has no tailnet identity,
so it is always token-only.

## Updating

Both binaries update themselves from this repo's GitHub Releases — download,
SHA-256-verify against the release `checksums.txt`, then atomically replace their
own executable. No GitHub token is needed (the repo is public); set `GITHUB_TOKEN`
only if you hit the unauthenticated API rate limit.

```sh
vmrc update --self                 # update this local vmrc CLI
vmrc update --self --check         # report the available version, install nothing
vmrc update --self --version v0.3.0  # pin a specific release (reinstall/downgrade)

vmrc update --all                  # drive every tag:vmrc agent to self-update
vmrc update buildbox,vm1           # update named agents
vmrc update --all --check          # report each agent's available version
```

On a VM you can also update the agent locally:

```sh
vmrc-agent update                  # stop → replace managed binary in bin/ → restart
vmrc-agent update --check          # report only
```

**Migrating a legacy system install to user mode:** if an older `--system`
(LocalSystem) install is present and user mode isn't set up yet, `vmrc-agent update`
performs a **one-time, idempotent migration** instead of an in-place swap: it copies
the config, `state/` (preserving the tailnet node identity — same node, no
re-enroll), and `files/` into `%LOCALAPPDATA%\vmrc`, registers the user logon agent,
and removes the system service. The migration runs locally with no network call, so
it works before the binary's release is fetchable.

Remote agent updates (`vmrc update --all`) drive each agent's authenticated
`/update` endpoint: the agent fetches the release itself, replaces its binary,
and exits so its boot service relaunches the new binary. A dev/source build
(`version == "dev"`) refuses to update unless you pass `--force` or
`--version <tag>`; prereleases are ignored unless you pass `--pre`.

## Driving VMs with an AI agent

`vmrc` is designed to be driven by an AI agent, not just a human. An in-repo
Claude Code skill teaches an agent the screenshot → reason → act loop over the
CLI — see [`.claude/skills/vmrc-cli/SKILL.md`](./.claude/skills/vmrc-cli/SKILL.md)
(auto-discovered by Claude Code when you open this repo). To reproduce the full
flow on a fresh VM, follow [docs/testing-on-a-vm.md](./docs/testing-on-a-vm.md).

## Install / build

**Quick install** — downloads the latest release for your OS/arch, SHA-256-verifies
it against the release `checksums.txt`, and puts the binary on your `PATH`:

```sh
# Host CLI (Linux/macOS):
curl -fsSL https://raw.githubusercontent.com/pddstudio/vmrc/main/scripts/install.sh | sh

# Agent on a VM (Linux/macOS) — installs vmrc-agent, then prints the setup step:
curl -fsSL https://raw.githubusercontent.com/pddstudio/vmrc/main/scripts/install.sh | VMRC_BINARY=vmrc-agent sh
```

```powershell
# Agent on a Windows VM (run from an Administrator PowerShell):
irm https://raw.githubusercontent.com/pddstudio/vmrc/main/scripts/install.ps1 | iex
```

The installer honors `VMRC_BINARY` (`vmrc`/`vmrc-agent`), `VMRC_VERSION` (pin a tag),
and `VMRC_BIN_DIR` (install location); the PowerShell script takes `-Binary`,
`-Version`, `-BinDir`. After the first install, the binary updates itself
(see [Updating](#updating)).

**From a release (manual):** download the archive for your OS/arch from the
[Releases](https://github.com/pddstudio/vmrc/releases) page, verify it against
`checksums.txt`, unpack, and place the binary on your `PATH`. For the agent,
then follow the `setup` quick-start above.

**From source (Make):**

```sh
make build         # both binaries → bin/ (version-stamped from git)
make dev-install   # build the CLI and symlink it onto your PATH (live rebuilds)
make check         # vet + test + lint (the CI gate); make help lists all targets
```

**From source (plain `go`):**

```sh
go build -o vmrc       ./cmd/vmrc          # host CLI
go build -o vmrc-agent ./cmd/vmrc-agent    # VM agent
vmrc --version                             # reports the build stamp
```

**All targets at once** (requires [GoReleaser](https://goreleaser.com)):

```sh
make snapshot                                 # win/linux/darwin × amd64/arm64 → dist/
make snapshot-host                            # just your host
# (equivalently: goreleaser build --snapshot --clean [--single-target])
```

> **macOS screenshots:** release `vmrc-agent` binaries are built with `CGO_ENABLED=0`,
> so on macOS `/screenshot` fails with HTTP 500 `no active displays` (`exec`/`get`/`put`/`read`
> work normally). Screen capture on macOS needs a cgo build with the macOS SDK — build
> `vmrc-agent` locally on a Mac (`go build ./cmd/vmrc-agent`) if you need it. Note that
> macOS capture also requires Screen-Recording (TCC) permission and a user-session
> service — see the runbook.

## Agent configuration

`vmrc-agent` reads config with precedence **flag → env → JSON file → default**.
See [`vmrc.example.json`](./vmrc.example.json) for a template.

| Key | Env override | Meaning |
|---|---|---|
| `hostname` | `VMRC_HOSTNAME` | tsnet node name on the tailnet; if empty/omitted, defaults to a DNS-sanitized form of the machine's OS hostname (falling back to `vmrc-agent`) |
| `state_dir` | `VMRC_STATE_DIR` | tsnet state dir (must be absolute) |
| `base_dir` | `VMRC_BASE_DIR` | root for `exec` cwd, `get`/`put`, `read` (must be absolute) |
| `read_globs` | — | allowlist of readable path globs under `base_dir` |
| `listen_port` | `VMRC_LISTEN_PORT` | HTTP listen port (default `47800`) |
| `allowed_logins` | — | identity allowlist: Tailscale login names |
| `allowed_tags` | — | identity allowlist: ACL tags, e.g. `tag:vmrc-admin` |

**Secrets are env-only and never read from or written to the JSON file:**

| Secret | Env var |
|---|---|
| tailnet auth key (enrollment) | `TS_AUTHKEY` |
| shared-token fallback | `VMRC_TOKEN` |

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
| [KICKOFF_PROMPT.md](./KICKOFF_PROMPT.md) | Contributor/AI build-session prompt — paste-ready to open a new build session |
| [CONTRIBUTING.md](./CONTRIBUTING.md) | How to build, test, lint, and contribute |
| [.claude/skills/vmrc-cli/SKILL.md](./.claude/skills/vmrc-cli/SKILL.md) | Claude Code skill: drive a VM fleet via the CLI |
| [docs/testing-on-a-vm.md](./docs/testing-on-a-vm.md) | End-to-end Windows VM test walkthrough |
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
