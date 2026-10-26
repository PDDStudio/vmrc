package agent

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)

// fakeWhoIs is a stub whoIser: it returns a canned response/error regardless of
// the queried address.
type fakeWhoIs struct {
	resp *apitype.WhoIsResponse
	err  error
}

func (f fakeWhoIs) WhoIs(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
	return f.resp, f.err
}

// userWho builds a WhoIs response for an untagged node owned by login.
func userWho(login string) *apitype.WhoIsResponse {
	return &apitype.WhoIsResponse{
		Node:        &tailcfg.Node{},
		UserProfile: &tailcfg.UserProfile{LoginName: login},
	}
}

// taggedWho builds a WhoIs response for a tagged node.
func taggedWho(tags ...string) *apitype.WhoIsResponse {
	return &apitype.WhoIsResponse{
		Node:        &tailcfg.Node{Tags: tags},
		UserProfile: &tailcfg.UserProfile{LoginName: "tagged-devices"},
	}
}

func TestIdentityAuthorizerAllowsAllowlistedLogin(t *testing.T) {
	a := IdentityAuthorizer{
		WhoIs:         fakeWhoIs{resp: userWho("alice@example.com")},
		AllowedLogins: []string{"alice@example.com"},
	}
	req := newClientRequest("POST", "/exec", nil)
	if err := a.Authorize(req); err != nil {
		t.Fatalf("allowlisted login must be authorized, got %v", err)
	}
}

func TestIdentityAuthorizerRejectsNonAllowlistedLogin(t *testing.T) {
	a := IdentityAuthorizer{
		WhoIs:         fakeWhoIs{resp: userWho("mallory@evil.com")},
		AllowedLogins: []string{"alice@example.com"},
	}
	req := newClientRequest("POST", "/exec", nil)
	err := a.Authorize(req)
	if err == nil {
		t.Fatal("non-allowlisted login must be denied")
	}
	if errors.Is(err, ErrUnidentified) {
		t.Fatalf("a resolved identity is a definitive deny, not ErrUnidentified: %v", err)
	}
}

func TestIdentityAuthorizerAllowsAllowlistedTag(t *testing.T) {
	a := IdentityAuthorizer{
		WhoIs:       fakeWhoIs{resp: taggedWho("tag:prod")},
		AllowedTags: []string{"tag:prod"},
	}
	req := newClientRequest("POST", "/exec", nil)
	if err := a.Authorize(req); err != nil {
		t.Fatalf("allowlisted tag must be authorized, got %v", err)
	}
}

func TestIdentityAuthorizerRejectsNonAllowlistedTag(t *testing.T) {
	a := IdentityAuthorizer{
		WhoIs:       fakeWhoIs{resp: taggedWho("tag:dev")},
		AllowedTags: []string{"tag:prod"},
	}
	req := newClientRequest("POST", "/exec", nil)
	err := a.Authorize(req)
	if err == nil {
		t.Fatal("non-allowlisted tag must be denied")
	}
	if errors.Is(err, ErrUnidentified) {
		t.Fatalf("a resolved tagged node is a definitive deny, not ErrUnidentified: %v", err)
	}
}

// A tagged node is authorized strictly by its tags; its login (the synthetic
// tag owner) must not satisfy the login allowlist.
func TestIdentityAuthorizerTaggedNodeIgnoresLoginAllowlist(t *testing.T) {
	a := IdentityAuthorizer{
		WhoIs:         fakeWhoIs{resp: taggedWho("tag:dev")},
		AllowedLogins: []string{"tagged-devices"},
		AllowedTags:   []string{"tag:prod"},
	}
	req := newClientRequest("POST", "/exec", nil)
	if err := a.Authorize(req); err == nil {
		t.Fatal("tagged node must be matched by tag, not login")
	}
}

func TestIdentityAuthorizerUnidentifiedReturnsErrUnidentified(t *testing.T) {
	a := IdentityAuthorizer{
		WhoIs:         fakeWhoIs{err: local.ErrPeerNotFound},
		AllowedLogins: []string{"alice@example.com"},
	}
	req := newClientRequest("POST", "/exec", nil)
	err := a.Authorize(req)
	if err == nil {
		t.Fatal("a WhoIs failure must not authorize")
	}
	if !errors.Is(err, ErrUnidentified) {
		t.Fatalf("an unresolvable peer must yield ErrUnidentified so a chain can fall back, got %v", err)
	}
}

func TestIdentityAuthorizerTransientWhoIsErrorDenies(t *testing.T) {
	// Only "no such peer" may fall through to the token. A transient LocalAPI
	// error for a peer that identity would deny must not open the token path.
	a := IdentityAuthorizer{
		WhoIs:         fakeWhoIs{err: errors.New("context deadline exceeded")},
		AllowedLogins: []string{"alice@example.com"},
	}
	err := a.Authorize(newClientRequest("POST", "/exec", nil))
	if err == nil || errors.Is(err, ErrUnidentified) {
		t.Fatalf("a transient WhoIs error must be a definitive deny, got %v", err)
	}
}

// funcAuthorizer adapts a no-arg func into an Authorizer for chain tests, so a
// test can observe whether a later authorizer was consulted.
type funcAuthorizer func() error

func (f funcAuthorizer) Authorize(_ *http.Request) error { return f() }

func TestChainAuthorizerAllowsWhenIdentityAllows(t *testing.T) {
	tokenCalled := false
	chain := ChainAuthorizer{Authorizers: []Authorizer{
		IdentityAuthorizer{WhoIs: fakeWhoIs{resp: userWho("alice@example.com")}, AllowedLogins: []string{"alice@example.com"}},
		funcAuthorizer(func() error { tokenCalled = true; return errors.New("token mismatch") }),
	}}
	req := newClientRequest("POST", "/exec", nil)
	if err := chain.Authorize(req); err != nil {
		t.Fatalf("chain must allow when identity allows, got %v", err)
	}
	if tokenCalled {
		t.Fatal("token authorizer must not be consulted once identity allows")
	}
}

// The done-check: a resolved-but-not-allowlisted identity is denied even when a
// later token authorizer would accept.
func TestChainAuthorizerDeniesResolvedIdentityEvenWithValidToken(t *testing.T) {
	tokenCalled := false
	chain := ChainAuthorizer{Authorizers: []Authorizer{
		IdentityAuthorizer{WhoIs: fakeWhoIs{resp: userWho("mallory@evil.com")}, AllowedLogins: []string{"alice@example.com"}},
		funcAuthorizer(func() error { tokenCalled = true; return nil }),
	}}
	req := newClientRequest("POST", "/exec", nil)
	if err := chain.Authorize(req); err == nil {
		t.Fatal("a resolved, non-allowlisted identity must be denied even with a valid token")
	}
	if tokenCalled {
		t.Fatal("a definitive identity deny must stop the chain before the token check")
	}
}

func TestChainAuthorizerFallsBackToTokenWhenUnidentified(t *testing.T) {
	chain := ChainAuthorizer{Authorizers: []Authorizer{
		IdentityAuthorizer{WhoIs: fakeWhoIs{err: local.ErrPeerNotFound}, AllowedLogins: []string{"alice@example.com"}},
		funcAuthorizer(func() error { return nil }),
	}}
	req := newClientRequest("POST", "/exec", nil)
	if err := chain.Authorize(req); err != nil {
		t.Fatalf("chain must fall back to token for an unidentified peer, got %v", err)
	}
}

func TestChainAuthorizerDeniesWhenUnidentifiedAndTokenWrong(t *testing.T) {
	chain := ChainAuthorizer{Authorizers: []Authorizer{
		IdentityAuthorizer{WhoIs: fakeWhoIs{err: local.ErrPeerNotFound}, AllowedLogins: []string{"alice@example.com"}},
		funcAuthorizer(func() error { return errors.New("bad token") }),
	}}
	req := newClientRequest("POST", "/exec", nil)
	if err := chain.Authorize(req); err == nil {
		t.Fatal("chain must deny when the peer is unidentified and the token is wrong")
	}
}

// With no allowlist configured, AuthorizerFor yields plain token behavior
// regardless of whether a WhoIs client is available.
func TestAuthorizerForNoAllowlistIsTokenOnly(t *testing.T) {
	a := AuthorizerFor(fakeWhoIs{resp: userWho("alice@example.com")}, nil, nil, "secret")
	good := newClientRequest("POST", "/exec?token=secret", nil)
	if err := a.Authorize(good); err != nil {
		t.Fatalf("right token must be authorized, got %v", err)
	}
	bad := newClientRequest("POST", "/exec?token=nope", nil)
	if err := a.Authorize(bad); err == nil {
		t.Fatal("wrong token must be denied")
	}
}

// With an allowlist and a WhoIs client, an allowlisted identity is authorized
// with no token at all.
func TestAuthorizerForAllowlistAllowsIdentityWithoutToken(t *testing.T) {
	a := AuthorizerFor(fakeWhoIs{resp: userWho("alice@example.com")}, []string{"alice@example.com"}, nil, "")
	req := newClientRequest("POST", "/exec", nil)
	if err := a.Authorize(req); err != nil {
		t.Fatalf("allowlisted identity must be authorized without a token, got %v", err)
	}
}

// With an allowlist, a resolved non-allowlisted identity is denied even with a
// valid token (identity governs).
func TestAuthorizerForAllowlistDeniesIdentityEvenWithToken(t *testing.T) {
	a := AuthorizerFor(fakeWhoIs{resp: userWho("mallory@evil.com")}, []string{"alice@example.com"}, nil, "secret")
	req := newClientRequest("POST", "/exec?token=secret", nil)
	if err := a.Authorize(req); err == nil {
		t.Fatal("non-allowlisted identity must be denied even with a valid token")
	}
}

// With an allowlist but no WhoIs client (e.g. --local dev mode), identity cannot
// be enforced, so AuthorizerFor falls back to token-only behavior.
func TestAuthorizerForAllowlistWithoutWhoIsIsTokenOnly(t *testing.T) {
	a := AuthorizerFor(nil, []string{"alice@example.com"}, nil, "secret")
	good := newClientRequest("POST", "/exec?token=secret", nil)
	if err := a.Authorize(good); err != nil {
		t.Fatalf("right token must be authorized when no WhoIs is available, got %v", err)
	}
	bad := newClientRequest("POST", "/exec?token=nope", nil)
	if err := a.Authorize(bad); err == nil {
		t.Fatal("wrong token must be denied")
	}
}
