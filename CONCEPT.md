# CONCEPT — vmrc

## Origin: a proven internal control agent

`vmrc` generalizes a feature that already exists and works as an internal tool: a small
HTTP **control agent** for driving a VM remotely.

In that earlier setting, a Windows VM ran a GUI application. Driving it remotely — taking
screenshots to check rendering, pulling crash/error logs, running build and packaging tooling,
launching/stopping the application — used to require RDP. The control agent replaced that with a
~270-line Go HTTP server that:

- runs on the VM and listens on `:47800`,
- is reachable only over Tailscale (the tailnet is the only route in),
- is gated by a shared token (`Authorization: Bearer …` header, constant-time compared; a
  legacy `?token=…` query is still accepted for backward compatibility),
- exposes 6 endpoints: `/screenshot`, `/read/` (logs), `/exec`, `/spawn`, `/get`, `/put`.

The operator on the host (and, crucially, an AI agent via a skill) drives the VM with plain
`curl` over the tunnel. It turned out to be a genuinely good interface for **both humans and
AI agents** to control a machine without bespoke remote-access plumbing — and other projects
need the same thing.

`vmrc` lifts that proven core out into a standalone, reusable tool.

## The capability set (end to end)

Inherited from the control agent, kept generic:

| Capability | Endpoint | Use |
|---|---|---|
| Desktop screenshot | `/screenshot` → PNG | see GUI state, debug rendering, confirm an app launched |
| Read a file / log | `/read/` (allowlisted) | tail crash/error logs without a shell |
| Run a command, wait | `/exec?cmd=…` → stdout + exit code | scripted operations, queries, builds |
| Spawn detached | `/spawn?cmd=…` → PID | launch GUI apps / long-running processes |
| Download a file | `/get?path=…` | pull build artifacts / outputs back to the host |
| Upload a file | `/put?path=…` (POST/PUT) | push scripts, inputs, tools onto the VM |

These are deliberately low-level primitives. Anything higher-level (build pipelines, app
control) composes from `exec` + `get`/`put` + `screenshot`.

### Who drives it

- **A human**, via the `vmrc` host CLI (`vmrc exec`, `vmrc shot`, `vmrc cp`, `vmrc ls`).
- **An AI agent** (like a Claude Code session), today via the CLI/curl, later via a native
  **MCP server mode** so the agent calls `vmrc` tools directly.

## What's new vs. the original control agent (the four deltas)

`vmrc` is not a copy — it generalizes along four axes:

1. **Self-contained tunnel.** The original assumes system Tailscale is already installed and
   running on the VM. `vmrc` **embeds `tsnet`**: the binary is its own tailnet node. Drop the
   binary + an auth key, and it joins the tailnet on start — nothing else to install.

2. **Boot autostart as a real service.** The original is launched ad-hoc (an external launcher,
   or a small run script). `vmrc` **installs itself as an OS service** (`vmrc-agent install`) so
   it starts in the background on every boot and restarts on failure.

3. **Fleet, not single-VM.** The original is one VM driven by curl. `vmrc` adds a **host CLI that
   enumerates and targets many VMs** (discovered via the tailnet, filtered by an ACL tag), so one
   command can hit one or several machines.

4. **Generic, not application-specific.** The original hardcodes an application-specific log
   allowlist and packaging assumptions. `vmrc` makes the base directory and allowed-read paths
   **configurable**, with no application-specific logic.

It also **upgrades the security model**: instead of relying solely on a shared token, `vmrc`
can authorize callers by their **Tailscale identity** (via tsnet's `WhoIs`), with the token as
an optional fallback. See [ARCHITECTURE.md](./ARCHITECTURE.md#security-model).

## Non-goals

- **Not a replacement for RDP/VNC** — no interactive desktop streaming; screenshots are
  point-in-time stills.
- **Not a config-management / orchestration platform** (no Ansible/Salt ambitions). It's a
  thin remote-primitives layer; orchestration is the caller's job.
- **Never exposed to the public internet.** The tailnet is the trust boundary. The agent must
  not be reachable off-tailnet.
- **Not a secrets store.** Auth keys and tokens are supplied at runtime (env/config), never
  committed.

## Why it generalizes cleanly

The control agent's core (`server.go`) is already application-agnostic except for the log
allowlist — `exec`/`spawn`/`get`/`put`/`screenshot` are universal VM primitives. The only
genuinely new engineering is the tunnel embed, the service installer, and the fleet CLI — all of
which are well-trodden Go territory (`tsnet`, `kardianos/service`). That's why the chosen path is
"lift the core, add three rings around it" rather than a rewrite.
