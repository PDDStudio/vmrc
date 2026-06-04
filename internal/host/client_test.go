package host

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newClient(srv *httptest.Server, token string) *Client {
	return &Client{BaseURL: srv.URL, HTTP: srv.Client(), Token: token}
}

func TestExecParsesOutputAndExitStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" || r.URL.Query().Get("cmd") != "echo hi" {
			t.Errorf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("X-Exit-Status", "3")
		_, _ = io.WriteString(w, "hi\n")
	}))
	defer srv.Close()
	out, status, err := newClient(srv, "").Exec(context.Background(), "echo hi")
	if err != nil {
		t.Fatalf("Exec err = %v", err)
	}
	if string(out) != "hi\n" || status != 3 {
		t.Fatalf("got out=%q status=%d", out, status)
	}
}

func TestTokenSentAsBearerHeaderNotQuery(t *testing.T) {
	var sawAuth, sawQueryToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawQueryToken = r.URL.Query().Get("token")
	}))
	defer srv.Close()
	_, _, _ = newClient(srv, "").Exec(context.Background(), "x")
	if sawAuth != "" {
		t.Fatalf("no token configured but Authorization=%q sent", sawAuth)
	}
	_, _, _ = newClient(srv, "sekret").Exec(context.Background(), "x")
	if sawAuth != "Bearer sekret" {
		t.Fatalf("token not sent as bearer header, got Authorization=%q", sawAuth)
	}
	if sawQueryToken != "" {
		t.Fatalf("token must not leak into the query string, got ?token=%q", sawQueryToken)
	}
}

func TestNon2xxMapsToError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	_, _, err := newClient(srv, "").Exec(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("want 403 error, got %v", err)
	}
}

func TestPutPostsBody(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Query().Get("path") != "out/a.txt" {
			t.Errorf("unexpected put: %s %s", r.Method, r.URL.RawQuery)
		}
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))
	defer srv.Close()
	if err := newClient(srv, "").Put(context.Background(), "out/a.txt", strings.NewReader("payload")); err != nil {
		t.Fatalf("Put err = %v", err)
	}
	if got != "payload" {
		t.Fatalf("body = %q", got)
	}
}

func TestClickPostsCoordinates(t *testing.T) {
	var method, path string
	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, q = r.Method, r.URL.Path, r.URL.Query()
	}))
	defer srv.Close()
	if err := newClient(srv, "").Click(context.Background(), 120, 240, "right", true); err != nil {
		t.Fatalf("Click err = %v", err)
	}
	if method != http.MethodPost || path != "/click" {
		t.Fatalf("got %s %s, want POST /click", method, path)
	}
	if q.Get("x") != "120" || q.Get("y") != "240" || q.Get("button") != "right" || q.Get("double") != "1" {
		t.Fatalf("query = %v", q)
	}
}

func TestClickOmitsDefaults(t *testing.T) {
	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
	}))
	defer srv.Close()
	if err := newClient(srv, "").Click(context.Background(), 1, 2, "", false); err != nil {
		t.Fatalf("Click err = %v", err)
	}
	if q.Has("button") || q.Has("double") {
		t.Fatalf("default button/double should be omitted, got %v", q)
	}
}

func TestTypePostsText(t *testing.T) {
	var method string
	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, q = r.Method, r.URL.Query()
	}))
	defer srv.Close()
	if err := newClient(srv, "").Type(context.Background(), "hello world"); err != nil {
		t.Fatalf("Type err = %v", err)
	}
	if method != http.MethodPost || q.Get("text") != "hello world" {
		t.Fatalf("got %s text=%q", method, q.Get("text"))
	}
}

func TestKeyPostsCombo(t *testing.T) {
	var method, path string
	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, q = r.Method, r.URL.Path, r.URL.Query()
	}))
	defer srv.Close()
	if err := newClient(srv, "").Key(context.Background(), "Ctrl+C"); err != nil {
		t.Fatalf("Key err = %v", err)
	}
	if method != http.MethodPost || path != "/key" || q.Get("combo") != "Ctrl+C" {
		t.Fatalf("got %s %s combo=%q", method, path, q.Get("combo"))
	}
}

func TestClickMapsNon2xxToError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no interactive session", http.StatusConflict)
	}))
	defer srv.Close()
	err := newClient(srv, "").Click(context.Background(), 1, 2, "left", false)
	if err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatalf("err = %v, want 409", err)
	}
}

func TestGetReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "filedata")
	}))
	defer srv.Close()
	rc, err := newClient(srv, "").Get(context.Background(), "a.txt")
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)
	if string(b) != "filedata" {
		t.Fatalf("body = %q", b)
	}
}
