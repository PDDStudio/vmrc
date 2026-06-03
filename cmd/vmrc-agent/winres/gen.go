// Package winres holds the Windows resource manifest for vmrc-agent.
//
// The agent embeds tsnet, which (via tailscale.com/net/tshttpproxy →
// github.com/dblohm7/wingoes) reads the true OS version at runtime. Without an
// application manifest declaring supportedOS compatibility, Windows hands the
// process a shimmed/"incoherent" version and wingoes panics during tunnel
// bring-up ("incoherent Windows version -- missing/outdated manifest?"),
// crashing the agent on start. winres.json carries the manifest; go-winres
// compiles it into rsrc_windows_<arch>.syso, which the Go linker embeds
// automatically into the windows build of cmd/vmrc-agent.
//
// Regenerate after editing winres.json:
//
//	go generate ./cmd/vmrc-agent/...
package winres

//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --in winres.json --out ../rsrc --arch amd64,arm64
