package agent

import (
	"crypto/subtle"
	"fmt"
	"net/http"
)

// Authorizer decides whether an inbound request may proceed. P0 ships a token
// implementation; P3 adds a tsnet-identity implementation behind the same
// interface, so handlers never change.
type Authorizer interface {
	// Authorize returns nil if the request is allowed, or an error describing
	// why it was denied.
	Authorize(r *http.Request) error
}

// TokenAuthorizer authorizes by a shared token passed as ?token=. An empty
// server token denies all requests (no accidental open access).
type TokenAuthorizer struct {
	Token string
}

func (a TokenAuthorizer) Authorize(r *http.Request) error {
	tok := r.URL.Query().Get("token")
	if a.Token == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(a.Token)) != 1 {
		return fmt.Errorf("bad or missing token")
	}
	return nil
}
