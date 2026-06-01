// Package tunnel embeds a Tailscale node (tsnet) so vmrc-agent joins the
// tailnet by itself and serves its HTTP API on a tailnet-only listener. It is
// imported by cmd/vmrc-agent; the agent core (internal/agent) stays
// tsnet-unaware so the same handler serves over either this tunnel or the
// plain --local listener.
package tunnel

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"tailscale.com/client/local"
	"tailscale.com/tsnet"
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

// HasState reports whether a tsnet state file already exists in dir, meaning the
// node can resume a prior identity without an auth key. Exported for the setup
// flow's enroll-skip decision.
func HasState(dir string) bool { return hasState(dir) }

// validateConfig fails fast on an un-enrollable configuration: no auth key and
// no saved state to resume from.
func validateConfig(cfg Config, stateExists bool) error {
	if cfg.AuthKey == "" && !stateExists {
		return fmt.Errorf("no TS_AUTHKEY and no saved tsnet state in %q; set TS_AUTHKEY to enroll the agent", cfg.StateDir)
	}
	return nil
}

// Tunnel is a running tsnet node with a serving listener.
type Tunnel struct {
	srv    *tsnet.Server
	ln     net.Listener
	lc     *local.Client
	scheme string // "https" or "http"
	fqdn   string
}

// Listener returns the tailnet listener to serve the agent handler on.
func (t *Tunnel) Listener() net.Listener { return t.ln }

// LocalClient returns the tsnet LocalClient (P3 identity auth reuses this).
func (t *Tunnel) LocalClient() *local.Client { return t.lc }

// Scheme reports the transport in use: "https" or "http".
func (t *Tunnel) Scheme() string { return t.scheme }

// FQDN returns the node's MagicDNS name (no trailing dot).
func (t *Tunnel) FQDN() string { return t.fqdn }

// Close stops the tsnet node.
func (t *Tunnel) Close() error { return t.srv.Close() }

// Up brings the tsnet node onto the tailnet and returns a Tunnel whose Listener
// is ready to serve. It picks HTTPS when the tailnet has a cert for this node,
// else plain HTTP. On any failure it tears down the partially-started node.
func Up(ctx context.Context, cfg Config) (*Tunnel, error) {
	if err := validateConfig(cfg, hasState(cfg.StateDir)); err != nil {
		return nil, err
	}
	srv := &tsnet.Server{
		Hostname: cfg.Hostname,
		AuthKey:  cfg.AuthKey,
		Dir:      cfg.StateDir,
	}
	status, err := srv.Up(ctx)
	if err != nil {
		_ = srv.Close()
		return nil, fmt.Errorf("tsnet up: %w", err)
	}
	fqdn := strings.TrimSuffix(status.Self.DNSName, ".")
	scheme, reason := selectScheme(status.CertDomains, fqdn)
	if reason != "" {
		log.Printf("tunnel: %s", reason)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	var ln net.Listener
	if scheme == "https" {
		ln, err = srv.ListenTLS("tcp", addr)
	} else {
		ln, err = srv.Listen("tcp", addr)
	}
	if err != nil {
		_ = srv.Close()
		return nil, fmt.Errorf("listen (%s) on %s: %w", scheme, addr, err)
	}

	lc, err := srv.LocalClient()
	if err != nil {
		_ = ln.Close()
		_ = srv.Close()
		return nil, fmt.Errorf("local client: %w", err)
	}
	return &Tunnel{srv: srv, ln: ln, lc: lc, scheme: scheme, fqdn: fqdn}, nil
}
