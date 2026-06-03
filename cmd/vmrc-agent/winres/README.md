# winres — Windows application manifest for vmrc-agent

This directory embeds a Win32 **application manifest** into the Windows builds
of `vmrc-agent`.

## Why it's needed

The agent embeds Tailscale's `tsnet`. On Windows, `tsnet`
(`tailscale.com/net/tshttpproxy` → `github.com/dblohm7/wingoes`) reads the true
OS version at runtime. A binary with **no manifest** is handed a *shimmed*
("incoherent") version by Windows' compatibility layer, and `wingoes` panics
during tunnel bring-up:

```
panic: incoherent Windows version -- missing/outdated manifest?
```

That panic fires in a tailscale-spawned goroutine, so it can't be recovered —
the agent crashes immediately on start. The manifest's `<supportedOS>`
compatibility GUIDs (Windows 7 → 10/11) tell Windows to report the real version,
which stops the panic. Every Windows binary using `tsnet` needs this.

## How it's wired

- `winres.json` — the manifest definition.
- `rsrc_windows_amd64.syso`, `rsrc_windows_arm64.syso` — the compiled resources,
  committed to the repo.
- The Go toolchain only auto-links `*.syso` from the **main** package directory.
  Because these live in this subpackage, `cmd/vmrc-agent/main.go` **blank-imports**
  this package (`_ ".../cmd/vmrc-agent/winres"`). That pulls it into the build
  graph and the linker embeds the resources. **Removing that import silently drops
  the manifest and brings the panic back.**

Ordinary `go build` and the goreleaser release pipeline consume the committed
`.syso` files — no extra tooling required.

## Regenerating

Only needed when editing `winres.json` (or adding an architecture):

```sh
go generate ./cmd/vmrc-agent/winres/
```

This runs [`go-winres`](https://github.com/tc-hib/go-winres) to rebuild the
`.syso` files. Commit the regenerated files.
