// Package tunnel embeds a Tailscale node (tsnet) so vmrc-agent joins the
// tailnet by itself and serves its HTTP API on a tailnet-only listener. It is
// imported by cmd/vmrc-agent; the agent core (internal/agent) stays
// tsnet-unaware so the same handler serves over either this tunnel or the
// plain --local listener.
package tunnel

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config is the subset of agent configuration the tunnel needs.
type Config struct {
	Hostname string // tsnet node name (VMRC_HOSTNAME)
	AuthKey  string // tailnet auth key (TS_AUTHKEY, env only)
	StateDir string // tsnet state dir (VMRC_STATE_DIR)
	Port     int    // listen port for the tailnet listener
}

// selectScheme decides the listener transport. HTTPS is used when the node's
// FQDN is among the tailnet's cert domains; otherwise HTTP, and reason explains
// the fallback. The tailnet is WireGuard-encrypted either way.
func selectScheme(certDomains []string, fqdn string) (scheme, reason string) {
	for _, d := range certDomains {
		if d == fqdn {
			return "https", ""
		}
	}
	return "http", fmt.Sprintf("HTTPS certs not enabled for %q; serving plain HTTP over the encrypted tailnet", fqdn)
}

// hasState reports whether a tsnet state file already exists in dir, meaning the
// node can resume a prior identity without an auth key. tsnet's default store is
// Dir/tailscaled.state.
func hasState(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "tailscaled.state"))
	return err == nil
}

// validateConfig fails fast on an un-enrollable configuration: no auth key and
// no saved state to resume from.
func validateConfig(cfg Config, stateExists bool) error {
	if cfg.AuthKey == "" && !stateExists {
		return fmt.Errorf("no TS_AUTHKEY and no saved tsnet state in %q; set TS_AUTHKEY to enroll the agent", cfg.StateDir)
	}
	return nil
}
