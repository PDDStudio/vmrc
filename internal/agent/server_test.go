package agent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeCapturer struct{ png []byte }

func (f fakeCapturer) Capture() ([]byte, error) { return f.png, nil }

func newTestServer(t *testing.T) (*Server, string) {
	base := t.TempDir()
	os.WriteFile(filepath.Join(base, "error.log"), []byte("boom"), 0o644)
	return &Server{Token: "secret", Capturer: fakeCapturer{png: []byte("PNGDATA")}, BaseDir: base}, base
}

func TestScreenshotRequiresToken(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/screenshot", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code = %d, want 403", rec.Code)
	}
}

func TestScreenshotOK(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/screenshot?token=secret", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "PNGDATA" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestLogsServesAllowlistedFile(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/logs/error.log?token=secret", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "boom" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestLogsBlocksTraversal(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/logs/..%2f..%2fsecret?token=secret", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("traversal must not return 200")
	}
}

func TestScreenshotWrongToken(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/screenshot?token=wrong", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong token: code = %d, want 403", rec.Code)
	}
}

func TestLogsBlocksNonAllowlisted(t *testing.T) {
	s, base := newTestServer(t)
	if err := os.WriteFile(filepath.Join(base, "passwd"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/logs/passwd?token=secret", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("non-allowlisted file must not be served (code=%d)", rec.Code)
	}
}

func TestAuthedRejectsEmptyServerToken(t *testing.T) {
	s := &Server{Token: "", Capturer: fakeCapturer{png: []byte("x")}, BaseDir: t.TempDir()}
	rec := httptest.NewRecorder()
	// Even an empty token in the request must be rejected when the server token is empty.
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/screenshot?token=", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("empty server token must always deny: code = %d, want 403", rec.Code)
	}
}

func TestExecRunsCommand(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/exec?token=secret&cmd=echo%20hello", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "hello" {
		t.Fatalf("body = %q, want %q", got, "hello")
	}
	if st := rec.Header().Get("X-Exit-Status"); st != "0" {
		t.Fatalf("X-Exit-Status = %q, want 0", st)
	}
}

func TestExecRequiresToken(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/exec?cmd=echo%20hello", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code = %d, want 403", rec.Code)
	}
}

func TestExecMissingCmd(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/exec?token=secret", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing cmd: code = %d, want 400", rec.Code)
	}
}

func TestExecNonzeroExitReported(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	// `exit 3` returns status 3 on the shell.
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/exec?token=secret&cmd=exit%203", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (output is returned even on nonzero exit)", rec.Code)
	}
	if st := rec.Header().Get("X-Exit-Status"); st != "3" {
		t.Fatalf("X-Exit-Status = %q, want 3", st)
	}
}

func TestGetServesFileWithinBase(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "out.bin"), []byte("PACKED"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/get?token=secret&path=out.bin", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "PACKED" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGetRequiresToken(t *testing.T) {
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/get?path=out.bin", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code = %d, want 403", rec.Code)
	}
}

func TestGetBlocksTraversal(t *testing.T) {
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/get?token=secret&path=../../../../etc/hosts", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("traversal must not return 200 (code=%d)", rec.Code)
	}
}

func TestSpawnReturnsPid(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/spawn?token=secret&cmd=exit%200", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spawned pid") {
		t.Fatalf("body = %q, want 'spawned pid ...'", rec.Body.String())
	}
}

func TestSpawnRequiresToken(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/spawn?cmd=exit%200", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code = %d, want 403", rec.Code)
	}
}

func TestPutWritesFileWithinBase(t *testing.T) {
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/put?token=secret&path=sub/up.txt", strings.NewReader("HELLO"))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(base, "sub", "up.txt"))
	if err != nil || string(got) != "HELLO" {
		t.Fatalf("file = %q err=%v", got, err)
	}
}

func TestPutRejectsGet(t *testing.T) {
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/put?token=secret&path=x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /put: code=%d, want 405", rec.Code)
	}
}

func TestPutBlocksTraversal(t *testing.T) {
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/put?token=secret&path=../escape.txt", strings.NewReader("x"))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("traversal must not write (code=%d)", rec.Code)
	}
}
