---
name: drive-vm
description: Use when you need to remote-control a VM (or a fleet of VMs) over the tailnet — run commands, capture and read the screen, and transfer files — via the vmrc host CLI.
---

# Driving VMs with vmrc

You control VMs through the `vmrc` host CLI, which talks to a `vmrc-agent`
running on each VM over a Tailscale tailnet. You operate the CLI; you do not
call any HTTP API directly.

## Prerequisites

- `vmrc` is on PATH (build: `go build -o vmrc ./cmd/vmrc`).
- Your machine is on the same tailnet as the agents (Tailscale running).
- Auth: agents that allowlist your tailnet identity need no token. Otherwise set
  `VMRC_TOKEN` or pass `--token`. Use `--scheme https` for TLS agents and `--tag`
  to target a non-default ACL tag.

## Discover the fleet

```sh
vmrc ls                 # list tag:vmrc agents on the tailnet
```
Pick a target by its name. Target one VM, several (`vm1,vm2`), or `--all`.

## The core loop: screenshot → reason → act

1. Capture the screen, then look at it:
   ```sh
   vmrc shot <vm> -o /tmp/<vm>.png
   ```
   Read `/tmp/<vm>.png` to see the desktop. (If a VM has no logged-in user, the
   agent returns a "no interactive session" error — fall back to `exec`.)
2. Decide the next action from what you see.
3. Run it and read the result (exit code is reported):
   ```sh
   vmrc exec <vm> -- <command>
   ```
4. Repeat until the goal is met. Re-screenshot to confirm GUI state changed.

## Commands

| Goal | Command |
|---|---|
| List agents | `vmrc ls` |
| Run a command (waits, returns exit code) | `vmrc exec <vm> -- <cmd>` |
| Run on every agent | `vmrc exec --all -- "<cmd>"` |
| Screenshot | `vmrc shot <vm> -o out.png` |
| Download a file | `vmrc get <vm>:<remote-path> <local-path>` |
| Upload a file | `vmrc put <local-path> <vm>:<remote-path>` |

Notes:
- With `--all` you MUST use the `--all -- <cmd>` form (the `--` separates flags
  from the command).
- Multi-target output is labeled per VM; non-zero exits are summarized on stderr.
- `exec` waits for completion; prefer it unless you explicitly need
  fire-and-forget behavior.

## Safety

- Commands run with the agent's privilege on the VM (often SYSTEM/root). Be
  deliberate; prefer reversible, scoped actions. Read before you write.
- Never print or log `TS_AUTHKEY` / `VMRC_TOKEN`.

## Installing this skill into Claude Code

Copy this directory into a skills location Claude Code loads (e.g. a plugin's
`skills/` directory, or a project `.claude/skills/drive-vm/`). The agent will
then invoke it whenever VM remote-control is needed.
