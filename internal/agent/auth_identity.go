package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
)

// ErrUnidentified signals that an Authorizer could not positively identify the
// caller (e.g. tsnet WhoIs found no peer for the remote address). A
// ChainAuthorizer treats it as "try the next authorizer"; any other error is a
// definitive denial that stops the chain — so a resolved-but-unauthorized
// identity is rejected even when a later authorizer (e.g. a token check) would
// have accepted.
var ErrUnidentified = errors.New("unidentified caller")

// whoIser is the slice of tsnet's LocalClient that IdentityAuthorizer needs.
// *local.Client satisfies it; tests inject a fake.
type whoIser interface {
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

// IdentityAuthorizer authorizes by Tailscale identity, resolved from the
// request's remote address via tsnet WhoIs, against an allowlist. A tagged node
// is matched against AllowedTags (any of its ACL tags); any other node is
// matched against AllowedLogins (the owner's login name). A peer that WhoIs
// cannot resolve yields ErrUnidentified so a ChainAuthorizer can fall back to
// token auth.
type IdentityAuthorizer struct {
	WhoIs         whoIser
	AllowedLogins []string
	AllowedTags   []string
}

func (a IdentityAuthorizer) Authorize(r *http.Request) error {
	who, err := a.WhoIs.WhoIs(r.Context(), r.RemoteAddr)
	if errors.Is(err, local.ErrPeerNotFound) {
		return fmt.Errorf("%w: whois %s: %v", ErrUnidentified, r.RemoteAddr, err)
	}
	if err != nil {
		// Any other failure (LocalAPI hiccup, netmap not yet received) is a deny,
		// not "unidentified": falling through would let the token admit a peer
		// that identity, a moment later, would reject.
		return fmt.Errorf("whois %s: %v", r.RemoteAddr, err)
	}
	if who == nil || who.Node == nil {
		return fmt.Errorf("%w: whois %s: empty response", ErrUnidentified, r.RemoteAddr)
	}
	// Tagged nodes carry no human owner, so they authorize strictly by tag.
	if who.Node.IsTagged() {
		for _, t := range who.Node.Tags {
			if slices.Contains(a.AllowedTags, t) {
				return nil
			}
		}
		return fmt.Errorf("tagged node %v not in allowed tags", who.Node.Tags)
	}
	login := ""
	if who.UserProfile != nil {
		login = who.UserProfile.LoginName
	}
	if slices.Contains(a.AllowedLogins, login) {
		return nil
	}
	return fmt.Errorf("login %q not in allowed logins", login)
}

// ChainAuthorizer tries each authorizer in order. The first to return nil
// allows the request. An authorizer returning ErrUnidentified falls through to
// the next; any other error is a definitive denial that stops the chain. The
// agent composes it as (identity, then token): identity governs whenever it can
// resolve the caller, and the token is only a fallback for unidentifiable peers.
type ChainAuthorizer struct {
	Authorizers []Authorizer
}

func (c ChainAuthorizer) Authorize(r *http.Request) error {
	lastErr := errors.New("no authorizers configured")
	for _, a := range c.Authorizers {
		err := a.Authorize(r)
		if err == nil {
			return nil
		}
		lastErr = err
		if !errors.Is(err, ErrUnidentified) {
			return err // definitive deny; do not fall through to a fallback
		}
	}
	return lastErr
}

// AuthorizerFor builds the agent's request authorizer from the configured
// identity allowlists and shared token. When an allowlist is set and a WhoIs
// client is available (tunnel mode), identity governs: the returned chain
// authorizes allowlisted peers (no token needed) and denies resolved-but-
// unauthorized peers outright, consulting the token only as a fallback for peers
// WhoIs cannot identify. With no allowlist, or no WhoIs client (e.g. --local dev
// mode where identity cannot be resolved), it falls back to plain token auth.
//
// who may be nil to signal that no WhoIs client is available.
func AuthorizerFor(who whoIser, allowedLogins, allowedTags []string, token string) Authorizer {
	hasAllowlist := len(allowedLogins) > 0 || len(allowedTags) > 0
	if hasAllowlist && who != nil {
		identity := IdentityAuthorizer{WhoIs: who, AllowedLogins: allowedLogins, AllowedTags: allowedTags}
		return ChainAuthorizer{Authorizers: []Authorizer{identity, TokenAuthorizer{Token: token}}}
	}
	return TokenAuthorizer{Token: token}
}
