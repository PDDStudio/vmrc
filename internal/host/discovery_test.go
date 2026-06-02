package host

import (
	"context"
	"errors"
	"testing"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
	"tailscale.com/types/views"
)

// fakeStatus is a stub statusSource returning a canned status/error.
type fakeStatus struct {
	st  *ipnstate.Status
	err error
}

func (f fakeStatus) Status(_ context.Context) (*ipnstate.Status, error) { return f.st, f.err }

// peer builds a tagged PeerStatus for tests.
func peer(dnsName, os string, online bool, tags ...string) *ipnstate.PeerStatus {
	tv := views.SliceOf(tags)
	return &ipnstate.PeerStatus{DNSName: dnsName, OS: os, Online: online, Tags: &tv}
}

func statusWith(peers ...*ipnstate.PeerStatus) *ipnstate.Status {
	m := map[key.NodePublic]*ipnstate.PeerStatus{}
	for _, p := range peers {
		m[key.NewNode().Public()] = p
	}
	return &ipnstate.Status{Peer: m}
}

func TestListFiltersByTagAndSorts(t *testing.T) {
	src := fakeStatus{st: statusWith(
		peer("zeta.example.ts.net.", "windows", true, "tag:vmrc"),
		peer("alpha.example.ts.net.", "linux", false, "tag:vmrc"),
		peer("other.example.ts.net.", "linux", true, "tag:web"), // filtered out
	)}
	agents, err := Discoverer{Source: src}.List(context.Background(), "tag:vmrc")
	if err != nil {
		t.Fatalf("List err = %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("want 2 agents, got %d (%v)", len(agents), agents)
	}
	if agents[0].Name != "alpha" || agents[1].Name != "zeta" {
		t.Fatalf("not sorted by name: %v", agents)
	}
	if agents[0].DNSName != "alpha.example.ts.net" {
		t.Fatalf("DNSName should drop trailing dot, got %q", agents[0].DNSName)
	}
}

func TestListDefaultsTagWhenEmpty(t *testing.T) {
	src := fakeStatus{st: statusWith(peer("a.example.ts.net.", "linux", true, "tag:vmrc"))}
	agents, err := Discoverer{Source: src}.List(context.Background(), "")
	if err != nil || len(agents) != 1 {
		t.Fatalf("empty tag should default to %q: agents=%v err=%v", DefaultTag, agents, err)
	}
}

func TestListWrapsDaemonError(t *testing.T) {
	src := fakeStatus{err: errors.New("dial unix: connect: connection refused")}
	_, err := Discoverer{Source: src}.List(context.Background(), "tag:vmrc")
	if err == nil {
		t.Fatal("a daemon error must propagate")
	}
}

func TestResolveHitMissAmbiguous(t *testing.T) {
	src := fakeStatus{st: statusWith(
		peer("vm1.example.ts.net.", "linux", true, "tag:vmrc"),
		peer("vm2.example.ts.net.", "linux", true, "tag:vmrc"),
	)}
	d := Discoverer{Source: src}
	if a, err := d.Resolve(context.Background(), "vm1", "tag:vmrc"); err != nil || a.Name != "vm1" {
		t.Fatalf("hit by short name failed: a=%v err=%v", a, err)
	}
	if _, err := d.Resolve(context.Background(), "vm1.example.ts.net", "tag:vmrc"); err != nil {
		t.Fatalf("hit by FQDN failed: %v", err)
	}
	if _, err := d.Resolve(context.Background(), "nope", "tag:vmrc"); err == nil {
		t.Fatal("miss must error")
	}
}
