# Runbook — vmrc-agent on Windows

The agent installs as a Windows service running as **LocalSystem** (privileged),
so it can fully control the VM (`exec`/`spawn`/`get`/`put`) and read the system
default paths.

## One-click setup

From an **elevated** PowerShell (Run as Administrator):

```powershell
$env:TS_AUTHKEY = "tskey-..."   # a tagged auth key from the Tailscale admin console
.\vmrc-agent.exe setup
```

`setup` writes `C:\ProgramData\vmrc\agent.json`, creates
`C:\ProgramData\vmrc\state` and `C:\ProgramData\vmrc\files`, enrolls the node on
your tailnet (using `TS_AUTHKEY` transiently — it is never written to disk),
then installs and starts the `vmrc-agent` service. After a reboot the agent
rejoins the tailnet automatically; no auth key is needed again (it resumes from
the persisted state). `setup` is safe to re-run.

## Lower-level commands

```powershell
.\vmrc-agent.exe run --local            # foreground dev mode (no service)
.\vmrc-agent.exe install                # register the service (needs prior enroll)
.\vmrc-agent.exe start | stop | status
.\vmrc-agent.exe uninstall
.\vmrc-agent.exe update                 # self-update: stop, replace binary, restart
.\vmrc-agent.exe update --check         # report the available release, install nothing
```

`install`/`start`/`stop`/`uninstall`/`setup`/`update` require an elevated shell; `status` does not.
`update` pulls the matching release from GitHub, verifies it, replaces the on-disk binary
(safe even while the service runs), and restarts the service so the new binary takes effect.

## State location

The tailnet identity lives in `C:\ProgramData\vmrc\state\tailscaled.state`. It
must be readable by LocalSystem — `C:\ProgramData` is by default. If you enroll
with `run` under a different account, make sure the state dir is one LocalSystem
can read (the default path is).

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
Remove-Item -Recurse C:\ProgramData\vmrc   # removes config + tailnet state + files
```

Removing the state dir also removes the node's tailnet identity; re-enroll with a
fresh `setup`.

## Linux

The same commands work on Linux via systemd (`sudo ./vmrc-agent setup`), with
defaults under `/etc/vmrc/agent.json` and `/var/lib/vmrc`. Screenshots need X11
(`DISPLAY`/`XAUTHORITY`); Wayland is unsupported. macOS is not yet verified.
