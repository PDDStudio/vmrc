# Runbook — vmrc-agent on Windows

By default the agent runs in the logged-in user's desktop session, started at logon
by a Scheduled Task with the user's elevated token (so it can capture the screen and
`exec` as admin). With `--system` it is instead a Windows service running as
**LocalSystem**, available before login but unable to see the desktop.

## One-click setup

From an **elevated** PowerShell (Run as Administrator):

```powershell
$env:TS_AUTHKEY = "tskey-..."   # a tagged auth key from the Tailscale admin console
.\vmrc-agent.exe setup            # user mode (default)
.\vmrc-agent.exe setup --system   # or: LocalSystem boot service
```

User mode writes `C:\ProgramData\vmrc\users\<user>\agent.json`, installs the binary in
`C:\ProgramData\vmrc\users\<user>\bin`, creates `%LOCALAPPDATA%\vmrc\state` and
`%LOCALAPPDATA%\vmrc\files`, and registers the logon task. `--system` writes
`C:\ProgramData\vmrc\agent.json`, creates `C:\ProgramData\vmrc\state` and
`C:\ProgramData\vmrc\files`, and installs the `vmrc-agent` service. Either way `setup`
enrolls the node on your tailnet (using `TS_AUTHKEY` transiently — it is never written
to disk) and starts the agent. After a reboot the agent rejoins the tailnet
automatically; no auth key is needed again (it resumes from the persisted state).
`setup` is safe to re-run. See [State location and permissions](#state-location-and-permissions)
for who can read and write these directories.

## Lower-level commands

```powershell
.\vmrc-agent.exe run --local            # foreground dev mode (no service)
.\vmrc-agent.exe install                # register the service (needs prior enroll)
.\vmrc-agent.exe start | stop | status
.\vmrc-agent.exe uninstall
.\vmrc-agent.exe update                 # self-update: stop, replace binary, restart
.\vmrc-agent.exe update --check         # report the available release, install nothing
```

With `--system`, `install`/`start`/`stop`/`uninstall`/`setup`/`update` require an elevated
shell; `status` does not. In the default user mode, `setup`/`install`/`uninstall`/`update`
require an elevated shell (the logon task runs elevated and its binary lives in an
admin-only directory, see below); `start`/`stop`/`status` do not. With `--no-elevate`
nothing but `uninstall` needs elevation.
`update` pulls the matching release from GitHub, verifies it, replaces the on-disk binary
(safe even while the service runs), and restarts the service so the new binary takes effect.

## State location and permissions

**System mode (`--system`).** Everything lives in `C:\ProgramData\vmrc`: `agent.json`,
`bin\vmrc-agent.exe`, the request log `logs\agent.log` (every `exec`/`spawn` command
line), and the tailnet identity `state\tailscaled.state` (the node's private key). By
default a folder under `C:\ProgramData` lets every local user read it and create files
in it, so `setup`/`install` give `C:\ProgramData\vmrc` an explicit **protected DACL**:

```
O:BA D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)
```

— owner Administrators, full control for SYSTEM and Administrators only, no inheritance
from `C:\ProgramData`; everything below is reset to inherit that (foreign owners and
explicit ACEs are removed; the `files\` base dir is reset but not walked). The agent
re-applies this on every start, which also repairs installs made before it existed.
Inspect it with `icacls C:\ProgramData\vmrc`. A junction/symlink at or inside that path
is refused rather than followed — delete it and re-run `setup`.

**User mode (default).** The logon task runs with your elevated token, so the files it
*executes or is configured by* must not be writable by your unelevated processes:

| What | Where |
|---|---|
| `agent.json`, `bin\vmrc-agent.exe`, `logs\agent.log` | `C:\ProgramData\vmrc\users\<user>\` — protected DACL as above **plus read & execute for `<user>`** |
| tailnet state, `base_dir` (`files\`) | `%LOCALAPPDATA%\vmrc\` (your profile) |

Edit `agent.json` from an elevated editor. With `--no-elevate` everything stays in
`%LOCALAPPDATA%\vmrc` (user-owned) and the task runs unelevated.

**Upgrading an older user-mode install** (config and binary in `%LOCALAPPDATA%\vmrc`,
task elevated): re-run `.\vmrc-agent.exe setup` from an elevated shell. It copies
`agent.json` to `C:\ProgramData\vmrc\users\<user>\` with `bin_dir` rewritten (review the
printed allowlist — the old file was user-writable), renames the old file to
`agent.json.migrated`, stops the running agent, installs the binary in the protected
`bin\`, re-registers the task, starts it, and deletes the old binary. Until you do, the
agent logs a warning on every start.

## Screenshots (user-session capture)

A Windows service runs in **session 0**, which has no interactive desktop. The
agent handles this automatically: when a screenshot is requested, the service
spawns a short-lived helper into the **active console session** (as the
logged-in user) that captures the desktop and streams it back. No extra setup
is required.

Requirement: a user must be logged in at the console. If no one is logged in
(e.g. the lock screen with no active session), `/screenshot` returns HTTP 409
`no interactive session` instead of an image. `exec`, `spawn`, `get`, `put`, and
`read` work regardless of login state.

### macOS screenshot capability

Release `vmrc-agent` binaries are built `CGO_ENABLED=0`. On macOS the screen-capture
backend needs cgo, so without it `screenshot.NumActiveDisplays()` reports `0` and
`/screenshot` fails with HTTP 500 `no active displays`. To enable capture, build on a Mac
with cgo (`go build ./cmd/vmrc-agent`), grant the binary (or its launching app)
Screen-Recording permission in System Settings → Privacy & Security, and run the agent
in a user session (a root LaunchDaemon cannot capture the screen). `exec`, `spawn`,
`get`, `put`, and `read` work regardless.

## Uninstall / cleanup

```powershell
.\vmrc-agent.exe stop
.\vmrc-agent.exe uninstall
Remove-Item -Recurse C:\ProgramData\vmrc   # --system: config + tailnet state + files
# user mode: Remove-Item -Recurse C:\ProgramData\vmrc\users\$env:USERNAME, $env:LOCALAPPDATA\vmrc
```

Removing the state dir also removes the node's tailnet identity; re-enroll with a
fresh `setup`.

## Linux

The same commands work on Linux via systemd. The boot service is
`sudo ./vmrc-agent setup --system`, with config in `/etc/vmrc/agent.json`, state and
files under `/var/lib/vmrc`, and the log in `/var/log/vmrc/agent.log`. The default user
mode installs a `systemd --user` unit and must be run **as that user, without sudo**
(`setup` refuses to run as root); it starts at login, or at boot once you run
`loginctl enable-linger "$USER"`. systemd restarts the agent 5 s after it exits
(including after a remote `/update`), and processes started via `/spawn` are stopped
together with the agent (`KillMode=control-group`). Screenshots need X11
(`DISPLAY`/`XAUTHORITY`); Wayland is unsupported. macOS is not yet verified.
