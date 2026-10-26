# Contributing to vmrc

Thanks for your interest in vmrc. This is a small Go project with two binaries
(`vmrc` host CLI, `vmrc-agent` VM agent) built from one module.

## Prerequisites

- Go (see the `go` directive in [`go.mod`](./go.mod) for the required version).
- For releases/cross-builds: [GoReleaser](https://goreleaser.com) v2.

## Build & run

A top-level `Makefile` wraps the common tasks (`make help` lists them):

```sh
make build        # both binaries → bin/ (version-stamped from git)
make dev-install  # build the host CLI and symlink it onto your PATH (live rebuilds)
make check        # vet + test + lint — the same gate CI runs
```

The underlying `go` commands, if you prefer them directly:

```sh
go build -o vmrc       ./cmd/vmrc          # host CLI
go build -o vmrc-agent ./cmd/vmrc-agent    # VM agent
./vmrc-agent run --local                   # foreground dev mode (plain TCP, no tunnel)
```

## Test, vet, lint

```sh
go test ./...
go vet ./...
# golangci-lint is pinned and run via `go run` under the module's Go toolchain
# (the prebuilt binary is built with an older Go and refuses this module):
GOTOOLCHAIN=go1.27.3 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...
```

Keep the lifted server tests green; add table-driven tests for new logic.

## Commit & branch conventions

- Conventional commit subjects: `type(scope): subject` (e.g. `feat(agent): ...`,
  `fix(host): ...`, `docs: ...`).
- Work on **feature branches**; open a PR. **Never push to `main` directly.**
  Merges into `main` are intentional (`--no-ff`).

## Dependencies

The dependency set is deliberately small (`tsnet`, `kardianos/service`,
`kbinani/screenshot`, `urfave/cli/v3`, `golang.org/x/sys`). **Open an issue
before adding a new dependency.**

## Release signing key

Releases sign `checksums.txt` with an ed25519 key, and the binaries refuse any
self-update that isn't signed by a public key in `internal/selfupdate/keys/*.pub`
(see [SECURITY.md](./SECURITY.md#release-signing)). Until a key is committed, release
builds can't self-update, and `release.yml` won't cut a release.

**One-time setup** (maintainer). Run from the repo root with `gh` authenticated:

```sh
go run ./tools/releasesign keygen -pub internal/selfupdate/keys/release.pub | gh secret set VMRC_RELEASE_SIGNING_KEY
git add internal/selfupdate/keys/release.pub   # commit the PUBLIC key via a PR
```

`keygen` writes only the public key to disk. It prints the private key (a base64
seed) to stdout, which the pipe feeds straight into the repository secret, so the
private key never touches disk or your screen. `keygen` refuses to print to a terminal
unless you pass `--force-tty`, and refuses to overwrite an existing `.pub` file. Keep an
offline backup of the private key only if you want one. Losing it just means rotating.

On release, goreleaser runs `go run ./tools/releasesign sign` with the secret in its
environment. `release.yml` first checks, before tagging, that the secret is set and
matches a committed public key.

**Planned rotation.** Every `.pub` file is trusted, so the new key goes in alongside
the old one:

1. Generate the next key under a **new** file name. Pipe the private key into your
   password manager's CLI for now, and leave the secret alone:
   `go run ./tools/releasesign keygen -pub internal/selfupdate/keys/release-2.pub | <store-in-password-manager>`
2. Commit `release-2.pub` next to the old `release.pub` and cut a release. The old key
   still signs it, and binaries that install it trust both keys.
3. Once your installs run that build, switch the secret:
   `<read-from-password-manager> | gh secret set VMRC_RELEASE_SIGNING_KEY`.
   From then on the new key signs releases.
4. Delete the old `release.pub` in a later release.

**Compromised key.** Generate the replacement straight into the secret
(`keygen -pub internal/selfupdate/keys/release-2.pub | gh secret set VMRC_RELEASE_SIGNING_KEY`),
delete the compromised `.pub` in the same commit, and release. Binaries that trust only
the compromised key can't verify the new release, by design. Reinstall those with
`scripts/install.sh` or `install.ps1`.

## Platforms

Targets are Windows, Linux, and macOS. Screen capture has platform caveats —
see [docs/runbook-windows.md](./docs/runbook-windows.md).

## Security

The agent's only ingress is the tailnet (tsnet listener) — never bind a public
address. Secrets (`TS_AUTHKEY`, `VMRC_TOKEN`) come from the environment and are
never committed.
