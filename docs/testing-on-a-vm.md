# Testing vmrc end-to-end on a Windows VM

This walks the full flow on a Windows VM: enroll the agent, drive it from your
host, and verify the user-session screenshot path.

## 0. Prerequisites

- A Windows VM you can open an elevated PowerShell on, with a user logged in at
  the console (needed for screenshots).
- Your host machine on the same tailnet (Tailscale running) with `vmrc` built:
  `go build -o vmrc ./cmd/vmrc`.
- A **tagged auth key** (`tag:vmrc`) from the Tailscale admin console.

## 1. Build / copy the agent onto the VM

Either download a release `vmrc-agent.exe` or build it
(`go build -o vmrc-agent.exe ./cmd/vmrc-agent`) and copy it to the VM.

## 2. Enroll + install (on the VM, elevated PowerShell)

```powershell
$env:TS_AUTHKEY = "tskey-..."
.\vmrc-agent.exe setup
```
`setup` defaults to **user-session mode**: it copies the binary into
`%LOCALAPPDATA%\vmrc\bin`, writes `%LOCALAPPDATA%\vmrc\agent.json`, enrolls the
node, and registers a **logon Scheduled Task** that runs the agent in your desktop
session (with highest privileges, so `exec` is elevated). Confirm it's up:
```powershell
.\vmrc-agent.exe status
```

To install the legacy LocalSystem boot service instead (no desktop capture; runs
before login), pass `--system`:
```powershell
.\vmrc-agent.exe setup --system   # writes C:\ProgramData\vmrc, registers a boot service
```

## 3. Drive it from the host

```sh
vmrc ls                                 # the VM appears as a tag:vmrc node
vmrc exec <vm> -- systeminfo            # runs, returns output + exit code
vmrc put ./hello.txt <vm>:hello.txt     # upload into the agent uploads dir (<base_dir>/uploads)
vmrc get <vm>:uploads/hello.txt ./hello.out   # round-trip download (get is base-dir relative)
```

## 4. Verify user-session screenshots + elevated exec

In user mode the agent runs **inside your desktop session**, so capture is a
direct grab (no session-0 bridge). With the agent running and the session
unlocked:
```sh
vmrc shot <vm> -o shot.png
```
Open `shot.png` — it must show your **actual desktop**. Confirm `exec` runs
elevated without a UAC prompt (the logon task has highest privileges):
```sh
vmrc exec <vm> -- whoami /groups        # should list the elevated/admin group as enabled
```
If the VM has multiple monitors or a very high resolution, confirm the capture
isn't truncated.

(`--system`-mode agents run in session 0 and cannot capture the desktop; that's
the deferred path.)

## 5. Logon persistence

Reboot the VM and log the user back in (user-mode agents start at **logon**, not
boot). Without re-running anything, confirm from the host that `vmrc ls` shows the
VM and `vmrc shot`/`vmrc exec` work — the logon task relaunched the agent, which
rejoined the tailnet from its saved `state/`. For unattended/headless VMs, enable
**auto-logon** so the desktop session (and the agent) come up with no human.

## Troubleshooting

- **Agent starts then immediately stops:** read the log file. The agent tees all
  output to `<config-dir>\logs\agent.log` — `%LOCALAPPDATA%\vmrc\logs\agent.log`
  in user mode, `C:\ProgramData\vmrc\logs\agent.log` with `--system` (on
  Linux/macOS it sits next to the config likewise). Each start writes a banner
  (version/pid/args); the line(s) after it — a config error, a tunnel-bringup
  failure, or a `PANIC:` stack — are the reason it exited. Override the path with
  `--log-file` / `VMRC_LOG_FILE`. The log rotates at `log_max_size_mb` (default
  10 MB) into `agent.log.1` … `agent.log.3`, so an older start may be in a backup.
- **Not in `vmrc ls`:** check `vmrc-agent.exe status` on the VM; confirm the auth
  key was tagged `tag:vmrc` and not expired; check Tailscale ACLs allow your host
  to reach port `47800`.
- **403 forbidden:** your identity isn't allowlisted and no valid token was sent
  — set `allowed_logins`/`allowed_tags`, or pass `--token` / `VMRC_TOKEN`.
- **Screenshot fails / blank (user mode):** confirm the session is **unlocked** —
  a locked workstation shows the secure desktop, which can't be captured. Confirm
  the logon task is actually running (`.\vmrc-agent.exe status`).
- **Screenshot can't work at all:** you likely installed with `--system` (the agent
  runs in session 0 and can't see the desktop). Use the default user mode instead.
- **State/permissions (user mode):** the working dir is `%LOCALAPPDATA%\vmrc`,
  owned by the logged-in user — no special ACLs needed. (`--system` mode uses
  `C:\ProgramData\vmrc`, readable by LocalSystem.)
