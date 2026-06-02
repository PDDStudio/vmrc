package host

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
		io.WriteString(w, "hi\n")
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

func TestTokenAppendedOnlyWhenSet(t *testing.T) {
	var sawToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawToken = r.URL.Query().Get("token")
	}))
	defer srv.Close()
	newClient(srv, "").Exec(context.Background(), "x")
	if sawToken != "" {
		t.Fatalf("no token configured but ?token=%q sent", sawToken)
	}
	newClient(srv, "sekret").Exec(context.Background(), "x")
	if sawToken != "sekret" {
		t.Fatalf("token not forwarded, got %q", sawToken)
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

func TestGetReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "filedata")
	}))
	defer srv.Close()
	rc, err := newClient(srv, "").Get(context.Background(), "a.txt")
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "filedata" {
		t.Fatalf("body = %q", b)
	}
}
