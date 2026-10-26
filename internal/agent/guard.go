package agent

import (
	"net/http"
	"slices"
	"strings"

	"github.com/pddstudio/vmrc/internal/protocol"
)

// ClientHeader must be present on every request (see protocol.ClientHeader).
// This is what stops a web page open on an allowlisted operator's machine from
// driving the agent: identity auth admits that machine, and an
// identity-authorized caller needs no token, so the token can't be the CSRF
// defense.
const ClientHeader = protocol.ClientHeader

// browserRequest reports why r looks like it came from a web browser, or "" if
// it doesn't. Origin and Sec-Fetch-* are set by browsers (and not by the CLI),
// including on same-origin requests after DNS rebinding, so their presence
// alone is grounds for refusal.
func browserRequest(r *http.Request) string {
	switch {
	case r.Header.Get(ClientHeader) == "":
		return "missing " + ClientHeader + " header"
	case r.Header.Get("Origin") != "":
		return "browser Origin header"
	case r.Header.Get("Sec-Fetch-Site") != "" || r.Header.Get("Sec-Fetch-Mode") != "":
		return "browser Sec-Fetch headers"
	}
	return ""
}

// guard wraps an endpoint with the browser check and a method allowlist. It
// runs before authorization so a forged request is refused regardless of who
// the caller's tailnet identity is.
func (s *Server) guard(methods []string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if why := browserRequest(r); why != "" {
			s.logf("DENIED  %s %s (%s)", r.Method, r.URL.Path, why)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !slices.Contains(methods, r.Method) {
			w.Header().Set("Allow", strings.Join(methods, ", "))
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

var (
	readOnly    = []string{http.MethodGet, http.MethodHead}
	sideEffects = []string{http.MethodPost}
	upload      = []string{http.MethodPost, http.MethodPut}
	update      = []string{http.MethodGet, http.MethodPost}
)
