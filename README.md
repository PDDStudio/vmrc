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
([install it below](#3-install-the-ai-agent-skill)).

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

Then enroll the VM by running `setup` with the auth key in the environment:

```sh
TS_AUTHKEY=tskey-... vmrc-agent setup
```

On Windows run it from an Administrator shell. On Linux/macOS run it **as the user
the agent should run as, without `sudo`** (user mode installs a per-user service;
`setup` refuses to run as root). For the machine-wide boot service use
`sudo TS_AUTHKEY=tskey-... vmrc-agent setup --system` instead. A Linux user service
only runs while that user has a session; to start it at boot run
`loginctl enable-linger "$USER"` once (`setup` prints this hint).

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
| Runs as | the logged-in user, in their desktop session | LocalSystem / root (boot service) |
| Autostart | logon (Windows Scheduled Task; macOS LaunchAgent; Linux `systemd --user`, at boot only with linger) | boot, before login |
| Config + binary (Windows) | `C:\ProgramData\vmrc\users\<user>\` (admin-only) | `C:\ProgramData\vmrc\` (admin-only) |
| State + `base_dir` (Windows) | `%LOCALAPPDATA%\vmrc\` (user-owned) | `C:\ProgramData\vmrc\` |
| Screen capture | ✅ captures the live desktop | ❌ session-0 isolated |
| `exec` privilege | the user, elevated (no UAC) — or unelevated with `--no-elevate` | `SYSTEM` / root |
| Available before login / at lock screen | ❌ (pair with auto-logon for headless) | ✅ |

User mode is the default because it can see and drive the desktop. On Windows its
logon task runs with the user's **elevated** token, so the config file and the binary
that task launches live in an admin-only directory (`C:\ProgramData\vmrc\users\<user>`,
protected DACL: SYSTEM + Administrators full control, the user read-only) — otherwise
any unelevated program of that user could swap them and be relaunched elevated.
Consequently `setup`, `install`, `uninstall` and `update` need an elevated shell, and
config edits must be made as Administrator. The tailnet state and `base_dir` stay in
`%LOCALAPPDATA%\vmrc`.

Pass `--no-elevate` to `setup`/`install` to opt out: the task runs with
`LeastPrivilege` (so `exec` is **not** elevated), everything lives in
`%LOCALAPPDATA%\vmrc`, and no elevation is needed to set up or update.

> **Upgrading an older Windows install:** installs made before this layout keep the
> config and binary in `%LOCALAPPDATA%\vmrc` while the task runs elevated. Re-run
> `vmrc-agent setup` from an elevated shell: it moves the config to the protected
> directory (the old file is kept as `agent.json.migrated`), installs the binary there,
> and re-registers the task. Until then the agent logs a warning on every start.

On macOS/Linux user mode keeps everything under the user's app-data dir
(`~/Library/Application Support/vmrc`, `~/.local/state/vmrc`). The managed binary lives
in `bin/` there and `update` replaces it. Under systemd (both modes) processes started
via `/spawn` belong to the agent's service and are stopped together with it on
restart, `update`, or `stop` (default `KillMode=control-group`).

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
vmrc get buildbox:C:\out\app.zip ./app.zip
vmrc put ./tool.exe buildbox:C:\tools\tool.exe
```

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

With no allowlist, the agent falls back to shared-token auth (`VMRC_TOKEN`); with
neither, every request is denied. The `--local` dev listener has no tailnet identity,
so it is always token-only.

## Updating

Both binaries update themselves from this repo's GitHub Releases — download,
SHA-256-verify against the release `checksums.txt`, then atomically replace their
own executable. No GitHub token is needed; set `GITHUB_TOKEN` only if you hit the
unauthenticated API rate limit.

```sh
vmrc update --self                   # update this local vmrc CLI
vmrc update --self --check           # report the available version, install nothing
vmrc update --self --version v0.3.0  # pin a specific release (reinstall/downgrade)

vmrc update --all                    # drive every tag:vmrc agent to self-update
vmrc update buildbox,vm1             # update named agents
vmrc update --all --check            # report each agent's available version
```

On a VM you can also update the agent locally:

```sh
vmrc-agent update                    # stop → replace managed binary in bin/ → restart
vmrc-agent update --check            # report only
```

(On Windows run it elevated unless the agent was installed with `--no-elevate`; on
Linux/macOS user mode run it as the agent's user, without `sudo`.) Before replacing the
binary the updater deletes any leftover `.vmrc-agent*.new` staging file and re-hashes
the staged file from disk, refusing the update if either step fails.

Remote agent updates (`vmrc update --all`) drive each agent's authenticated `/update`
endpoint: the agent fetches the release itself, replaces its binary, and exits so its
service relaunches the new binary. A dev/source build (`version == "dev"`) refuses to
update unless you pass `--force` or `--version <tag>`; prereleases are ignored unless
you pass `--pre`.

## Agent configuration

`vmrc-agent` reads config with precedence **flag → env → JSON file → default**.
See [`vmrc.example.json`](./vmrc.example.json) for a template.

| Key | Env override | Meaning |
|---|---|---|
| `hostname` | `VMRC_HOSTNAME` | tsnet node name on the tailnet; if empty/omitted, defaults to a DNS-sanitized form of the machine's OS hostname (falling back to `vmrc-agent`) |
| `state_dir` | `VMRC_STATE_DIR` | tsnet state dir (must be absolute) |
| `base_dir` | `VMRC_BASE_DIR` | root for `exec` cwd, `get`/`put`, `read` (must be absolute) |
| `bin_dir` | — | dir holding the managed agent binary for self-update; defaults to a `bin/` beside the config file. For an elevated Windows user-mode install it must stay inside `C:\ProgramData\vmrc\users\<user>` (setup refuses otherwise) |
| `read_globs` | — | allowlist of readable path globs under `base_dir` |
| `listen_port` | `VMRC_LISTEN_PORT` | HTTP listen port (default `47800`) |
| `allowed_logins` | — | identity allowlist: Tailscale login names |
| `allowed_tags` | — | identity allowlist: ACL tags, e.g. `tag:vmrc-admin` |

**Secrets are env-only and never read from or written to the JSON file:**

| Secret | Env var |
|---|---|
| tailnet auth key (enrollment) | `TS_AUTHKEY` |
| shared-token fallback | `VMRC_TOKEN` |

### Agent log

The agent logs every request — **including the full command line of each `exec` and
`spawn`** — to `<config-dir>/logs/agent.log` (Linux system mode:
`/var/log/vmrc/agent.log`; override with `--log-file` / `VMRC_LOG_FILE`). Don't put
secrets on remote command lines. The log is created owner-only (dir `0700`, file
`0600`; on Windows it sits in the admin-only install dir).

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
| [docs/testing-on-a-vm.md](./docs/testing-on-a-vm.md) | End-to-end Windows VM test walkthrough |
| [docs/runbook-windows.md](./docs/runbook-windows.md) | Windows install/enroll/operate runbook |

## License

[MIT](./LICENSE).
