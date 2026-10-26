package agent

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

// Authorizer decides whether an inbound request may proceed. P0 ships a token
// implementation; P3 adds a tsnet-identity implementation behind the same
// interface, so handlers never change.
type Authorizer interface {
	// Authorize returns nil if the request is allowed, or an error describing
	// why it was denied.
	Authorize(r *http.Request) error
}

// TokenAuthorizer authorizes by a shared token. The token is read from an
// "Authorization: Bearer <token>" header only — a ?token= query parameter is
// not accepted, since tokens in URLs end up in logs and proxies. An empty
// server token denies all requests (no accidental open access).
type TokenAuthorizer struct {
	Token string
}

func (a TokenAuthorizer) Authorize(r *http.Request) error {
	if a.Token == "" || subtle.ConstantTimeCompare([]byte(requestToken(r)), []byte(a.Token)) != 1 {
		return fmt.Errorf("bad or missing token")
	}
	return nil
}

// requestToken extracts the shared token from the Authorization: Bearer header.
func requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if rest, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
