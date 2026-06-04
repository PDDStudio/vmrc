// Package winres carries the Windows application manifest for vmrc-agent.
//
// # Why this package exists
//
// The agent embeds tsnet, which (via tailscale.com/net/tshttpproxy →
// github.com/dblohm7/wingoes) reads the true OS version at runtime. Without an
// application manifest declaring supportedOS compatibility, Windows hands the
// process a shimmed/"incoherent" version, wingoes panics during tunnel
// bring-up ("incoherent Windows version -- missing/outdated manifest?"), and
// the agent dies on start. winres.json carries the manifest; go-winres compiles
// it into rsrc_windows_<arch>.syso in this directory.
//
// # How it gets linked
//
// The Go toolchain only auto-links *.syso from the *main* package directory.
// These .syso files live here in a subpackage, so cmd/vmrc-agent blank-imports
// this package; that pulls winres into the build graph and the linker embeds
// the .syso resources into the windows build. If that import is removed, the
// manifest is silently dropped and the startup panic returns.
//
// How to regenerate (after editing winres.json, or to add an arch):
//
//	go generate ./cmd/vmrc-agent/winres/
//
// go-winres is only needed at regeneration time; ordinary `go build` and the
// goreleaser pipeline consume the committed .syso files and need no extra tools.
package winres

//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --in winres.json --out rsrc --arch amd64,arm64
