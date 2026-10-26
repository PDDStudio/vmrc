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

## Platforms

Targets are Windows, Linux, and macOS. Screen capture has platform caveats —
see [docs/runbook-windows.md](./docs/runbook-windows.md).

## Security

The agent's only ingress is the tailnet (tsnet listener) — never bind a public
address. Secrets (`TS_AUTHKEY`, `VMRC_TOKEN`) come from the environment and are
never committed.
