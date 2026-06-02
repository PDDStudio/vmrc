package host

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"tailscale.com/ipn/ipnstate"
)

// DefaultTag is the ACL tag identifying vmrc agents on the tailnet.
const DefaultTag = "tag:vmrc"

// Agent is a discovered vmrc node on the tailnet.
type Agent struct {
	Name    string       // MagicDNS short name (display + addressing)
	DNSName string       // FQDN without trailing dot (used to build the base URL)
	IPs     []netip.Addr // tailnet IPs (fallback addressing)
	Online  bool
	OS      string
}

// statusSource is the minimal interface extracted from tailscale.com/client/local.Client
// that discovery needs; *local.Client satisfies it. Tests inject a fake.
type statusSource interface {
	Status(ctx context.Context) (*ipnstate.Status, error)
}

// Discoverer enumerates vmrc agents from a Tailscale status source.
type Discoverer struct {
	Source statusSource
}

// List returns every agent carrying tag (default DefaultTag when empty), sorted
// by name.
func (d Discoverer) List(ctx context.Context, tag string) ([]Agent, error) {
	if tag == "" {
		tag = DefaultTag
	}
	st, err := d.Source.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not reach the local Tailscale daemon; is Tailscale running? (%w)", err)
	}
	var agents []Agent
	for _, p := range st.Peer {
		if p.Tags == nil || !p.Tags.ContainsFunc(func(s string) bool { return s == tag }) {
			continue
		}
		agents = append(agents, agentFromPeer(p))
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}

func agentFromPeer(p *ipnstate.PeerStatus) Agent {
	fqdn := strings.TrimSuffix(p.DNSName, ".")
	name := fqdn
	if i := strings.IndexByte(fqdn, '.'); i > 0 {
		name = fqdn[:i]
	}
	if name == "" {
		name = p.HostName
	}
	return Agent{Name: name, DNSName: fqdn, IPs: p.TailscaleIPs, Online: p.Online, OS: p.OS}
}

// Resolve returns the single agent whose short name or FQDN matches name. A miss
// lists the available agents; an ambiguous match is an error.
func (d Discoverer) Resolve(ctx context.Context, name, tag string) (Agent, error) {
	agents, err := d.List(ctx, tag)
	if err != nil {
		return Agent{}, err
	}
	var matches []Agent
	for _, a := range agents {
		if a.Name == name || a.DNSName == name {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Agent{}, fmt.Errorf("no vmrc agent named %q; available: %s", name, agentNames(agents))
	default:
		return Agent{}, fmt.Errorf("target %q is ambiguous (%d matches)", name, len(matches))
	}
}

func agentNames(agents []Agent) string {
	if len(agents) == 0 {
		return "(none)"
	}
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}
