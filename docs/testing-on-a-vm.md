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
This writes `C:\ProgramData\vmrc\agent.json`, enrolls the node, and installs +
starts the boot service. Confirm it's up:
```powershell
.\vmrc-agent.exe status
```

## 3. Drive it from the host

```sh
vmrc ls                                 # the VM appears as a tag:vmrc node
vmrc exec <vm> -- systeminfo            # runs, returns output + exit code
vmrc put ./hello.txt <vm>:hello.txt     # upload into the agent base dir
vmrc get <vm>:hello.txt ./hello.out     # round-trip download
```

## 4. Verify user-session screenshots (the new capture path)

With a user logged in at the VM console:
```sh
vmrc shot <vm> -o shot.png
```
Open `shot.png` — it must show the **logged-in user's actual desktop**, not a
black/empty session-0 frame. This confirms the service bridged into the active
session. If the VM has multiple monitors or a very high resolution, confirm the
capture streams back fully (the PNG is read over a pipe before the helper is
reaped) and is not truncated.

Now lock the screen / sign the user out so no console session is active, and
retry:
```sh
vmrc shot <vm> -o shot2.png
```
Expected: an error with HTTP **409 "no interactive session"** (not a 500 and not
a blank image). `vmrc exec <vm> -- whoami` still works.

## 5. Reboot persistence

Reboot the VM. Without logging in again to re-run anything, confirm from the
host that `vmrc ls` shows the VM and `vmrc exec <vm> -- whoami` works — the agent
rejoined the tailnet on boot. After a user logs in, `vmrc shot` works again.

## Troubleshooting

- **Not in `vmrc ls`:** check `vmrc-agent.exe status` on the VM; confirm the auth
  key was tagged `tag:vmrc` and not expired; check Tailscale ACLs allow your host
  to reach port `47800`.
- **403 forbidden:** your identity isn't allowlisted and no valid token was sent
  — set `allowed_logins`/`allowed_tags`, or pass `--token` / `VMRC_TOKEN`.
- **Screenshot 409 with a user present:** confirm a user is actually logged in at
  the *console* (not just an RDP session that has since disconnected).
- **Screenshot 500 (not 409) on capture:** the bridge spawns the capture helper
  via the logged-in user's token, which requires the service to run as
  **LocalSystem** (the default install). If you run the agent under a non-SYSTEM
  account, acquiring the user token fails — reinstall via `setup` so the service
  runs as LocalSystem.
- **State/permissions:** tailnet identity lives in `C:\ProgramData\vmrc\state`;
  it must be readable by LocalSystem (the default path is).
