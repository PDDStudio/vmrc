package agent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newClientRequest builds a request the way the vmrc host client sends it: the
// X-Vmrc-Client header set, and a ?token= in target moved into an
// Authorization: Bearer header (the agent no longer reads query tokens).
func newClientRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Header.Set(ClientHeader, "test")
	q := r.URL.Query()
	if tok := q.Get("token"); tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
		q.Del("token")
		r.URL.RawQuery = q.Encode()
	}
	return r
}

func TestGuardRejectsBrowserRequests(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(r *http.Request)
	}{
		{"missing client header", func(r *http.Request) { r.Header.Del(ClientHeader) }},
		{"cross-origin Origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }},
		{"same-origin Origin (DNS rebinding)", func(r *http.Request) { r.Header.Set("Origin", "https://vm.example.ts.net:47800") }},
		{"img/no-cors fetch metadata", func(r *http.Request) {
			r.Header.Set("Sec-Fetch-Mode", "no-cors")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		}},
		{"same-origin fetch metadata", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, base := newTestServer(t)
			marker := base + "/pwned"
			r := newClientRequest("POST", "/exec?token=secret&cmd="+url.QueryEscape("touch "+marker), nil)
			tc.mutate(r)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, r)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("code = %d, want 403", rec.Code)
			}
			if _, err := http.Dir(base).Open("/pwned"); err == nil {
				t.Fatal("command ran despite the request being rejected")
			}
		})
	}
}

func TestGuardAllowsCLIRequest(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/exec?token=secret&cmd=echo+hi", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "hi" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGuardRunsBeforeAuthLogging(t *testing.T) {
	// A browser-shaped request must be refused even with a valid token: an
	// allowlisted identity needs no token, so the token can't be the CSRF defense.
	s, _ := newTestServer(t)
	r := newClientRequest("GET", "/screenshot?token=secret", nil)
	r.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}
}

func TestEndpointMethods(t *testing.T) {
	cases := []struct {
		target  string
		allowed []string
		denied  []string
	}{
		{"/exec?cmd=true", []string{"POST"}, []string{"GET", "PUT", "DELETE"}},
		{"/spawn?cmd=true", []string{"POST"}, []string{"GET"}},
		{"/click?x=1&y=1", []string{"POST"}, []string{"GET"}},
		{"/type?text=a", []string{"POST"}, []string{"GET"}},
		{"/key?combo=Enter", []string{"POST"}, []string{"GET"}},
		{"/put?path=m.txt", []string{"POST", "PUT"}, []string{"GET"}},
		{"/screenshot", []string{"GET"}, []string{"POST"}},
		{"/get?path=error.log", []string{"GET"}, []string{"POST"}},
		{"/read/error.log", []string{"GET"}, []string{"POST"}},
	}
	for _, tc := range cases {
		for _, m := range tc.denied {
			t.Run(m+" "+tc.target, func(t *testing.T) {
				s, _ := newTestServer(t)
				s.Injector = &fakeInjector{}
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, newClientRequest(m, tc.target+sep(tc.target)+"token=secret", nil))
				if rec.Code != http.StatusMethodNotAllowed {
					t.Fatalf("code = %d, want 405", rec.Code)
				}
			})
		}
		for _, m := range tc.allowed {
			t.Run(m+" "+tc.target, func(t *testing.T) {
				s, _ := newTestServer(t)
				s.Injector = &fakeInjector{}
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, newClientRequest(m, tc.target+sep(tc.target)+"token=secret", strings.NewReader("x")))
				if rec.Code == http.StatusMethodNotAllowed {
					t.Fatalf("%s must be allowed", m)
				}
			})
		}
	}
}

func sep(target string) string {
	if strings.Contains(target, "?") {
		return "&"
	}
	return "?"
}

func TestQueryTokenNoLongerAccepted(t *testing.T) {
	s, _ := newTestServer(t)
	r := httptest.NewRequest("GET", "/screenshot?token=secret", nil)
	r.Header.Set(ClientHeader, "test")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("?token= must no longer authorize (tokens in URLs leak into logs): code=%d", rec.Code)
	}
}
