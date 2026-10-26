---
name: vmrc-cli
description: Use to remote-control a VM (or fleet) over the tailnet via the vmrc host CLI — drive the GUI desktop (screenshot, click the mouse, type text, press keys like Enter/Ctrl+C), run shell commands, and transfer files. Trigger for any "control/automate/click/type on the VM", "take a screenshot of the VM", "fill in that dialog", "run X on the VM", or "copy a file to/from the VM" task.
---

# Driving VMs with the vmrc CLI

You control VMs through the `vmrc` host CLI, which talks to a `vmrc-agent`
running on each VM over a Tailscale tailnet. You operate the CLI; you do not
call any HTTP API directly. The CLI can both **run shell commands** and **drive
the GUI** (mouse + keyboard), so you can automate desktop apps, not just the
shell.

## Prerequisites

- `vmrc` is on PATH (build: `go build -o vmrc ./cmd/vmrc`).
- Your machine is on the same tailnet as the agents (Tailscale running).
- Auth: agents that allowlist your tailnet identity need no token. Otherwise set
  `VMRC_TOKEN` or pass `--token`. The CLI uses HTTPS by default; pass `--scheme http`
  for agents on a tailnet without HTTPS certs, and `--tag` to target a non-default ACL tag.

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
2. Decide the next action from what you see, reading pixel coordinates off the
   screenshot.
3. **Act**, then read the result:
   - GUI: `vmrc click <vm> <x> <y>`, `vmrc type <vm> "<text>"`,
     `vmrc key <vm> "<combo>"` (e.g. `Enter`, `Tab`, `Ctrl+C`).
   - Shell: `vmrc exec <vm> -- <command>` (waits, reports exit code).
4. Re-screenshot to confirm the state changed, then repeat until the goal is met.

Worked GUI example — type into a field and submit:
```sh
vmrc shot win1 -o /tmp/win1.png        # find the input box, say at (640,300)
vmrc click win1 640 300                # focus it
vmrc type  win1 "hello world"          # type literal text
vmrc key   win1 "Enter"                # submit
vmrc shot win1 -o /tmp/win1.png        # confirm
```

## Commands

| Goal | Command |
|---|---|
| List agents | `vmrc ls` |
| Run a command (waits, returns exit code) | `vmrc exec <vm> -- <cmd>` |
| Run on every agent | `vmrc exec --all -- "<cmd>"` |
| Screenshot | `vmrc shot <vm> -o out.png` |
| Click the mouse | `vmrc click <vm> <x> <y> [--button left\|right\|middle] [--double]` |
| Type literal text | `vmrc type <vm> "<text>"` |
| Press a key / combo | `vmrc key <vm> "<combo>"` (e.g. `Enter`, `Tab`, `Esc`, `Ctrl+C`, `Alt+F4`) |
| Download a file | `vmrc get <vm>:<remote-path> <local-path>` |
| Upload a file | `vmrc put <local-path> <vm>:<remote-path>` (lands in the agent's uploads dir; `exec` sees it at `uploads/<remote-path>`; auto-deleted after the retention, default 7 days) |

Notes:
- With `--all` you MUST use the `--all -- <cmd>` form (the `--` separates flags
  from the command).
- Multi-target output is labeled per VM; non-zero exits are summarized on stderr.
- `exec` waits for completion and returns the exit code; for a long-running GUI
  app, launch it in the background from within the command (e.g. Windows
  `start ""`) so the request returns.
- `click`/`type`/`key` are **Windows-only today** (macOS/Linux return "not
  supported") and need a logged-in interactive session — the same "no
  interactive session" caveat as `shot`. Click coordinates are absolute screen
  pixels matching the screenshot.

## Safety

- Commands run with the agent's privilege on the VM (often SYSTEM/root). Be
  deliberate; prefer reversible, scoped actions. Read before you write.
- Never print or log `TS_AUTHKEY` / `VMRC_TOKEN`.

## Where this skill lives

This skill ships in the repo at `.claude/skills/vmrc-cli/`, so Claude Code
auto-discovers it whenever you open this project. To use it from another
workspace, copy this directory into that project's `.claude/skills/`, or into a
user-level (`~/.claude/skills/`) or plugin skills directory.
