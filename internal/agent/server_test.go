package agent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeCapturer struct{ png []byte }

func (f fakeCapturer) Capture() ([]byte, error) { return f.png, nil }

func newTestServer(t *testing.T) (*Server, string) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "error.log"), []byte("boom"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Server{Token: "secret", Capturer: fakeCapturer{png: []byte("PNGDATA")}, BaseDir: base, ReadGlobs: []string{"*.log"}}, base
}

func TestScreenshotRequiresToken(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/screenshot", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code = %d, want 403", rec.Code)
	}
}

func TestScreenshotOK(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/screenshot?token=secret", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "PNGDATA" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestReadServesGlobMatchedFile(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/read/error.log?token=secret", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "boom" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestLogsAliasStillServes(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/logs/error.log?token=secret", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "boom" {
		t.Fatalf("alias /logs/ should still serve: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestReadBlocksNonMatchingGlob(t *testing.T) {
	s, base := newTestServer(t)
	if err := os.WriteFile(filepath.Join(base, "passwd"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/read/passwd?token=secret", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("file not matching any glob must not be served (code=%d)", rec.Code)
	}
}

func TestReadBlocksTraversal(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/read/..%2f..%2fsecret?token=secret", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("traversal must not return 200")
	}
}

func TestReadRequiresToken(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/read/error.log", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code = %d, want 403", rec.Code)
	}
}

func TestScreenshotWrongToken(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/screenshot?token=wrong", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong token: code = %d, want 403", rec.Code)
	}
}

func TestAuthedRejectsEmptyServerToken(t *testing.T) {
	s := &Server{Token: "", Capturer: fakeCapturer{png: []byte("x")}, BaseDir: t.TempDir()}
	rec := httptest.NewRecorder()
	// Even an empty token in the request must be rejected when the server token is empty.
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/screenshot?token=", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("empty server token must always deny: code = %d, want 403", rec.Code)
	}
}

func TestExecRunsCommand(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/exec?token=secret&cmd=echo%20hello", nil))
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
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/exec?cmd=echo%20hello", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code = %d, want 403", rec.Code)
	}
}

func TestExecMissingCmd(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/exec?token=secret", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing cmd: code = %d, want 400", rec.Code)
	}
}

func TestExecNonzeroExitReported(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	// `exit 3` returns status 3 on the shell.
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/exec?token=secret&cmd=exit%203", nil))
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
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/get?token=secret&path=out.bin", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "PACKED" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGetRequiresToken(t *testing.T) {
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/get?path=out.bin", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code = %d, want 403", rec.Code)
	}
}

func TestGetBlocksTraversal(t *testing.T) {
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/get?token=secret&path=../../../../etc/hosts", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("traversal must not return 200 (code=%d)", rec.Code)
	}
}

func TestSpawnReturnsPid(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/spawn?token=secret&cmd=exit%200", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spawned pid") {
		t.Fatalf("body = %q, want 'spawned pid ...'", rec.Body.String())
	}
}

func TestSpawnReapsChild(t *testing.T) {
	s, _ := newTestServer(t)
	reaped := make(chan int, 1)
	s.spawnReaped = func(pid int) { reaped <- pid }
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/spawn?token=secret&cmd=exit%200", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	// The child must be waited on so it does not linger as a zombie (on Unix a
	// detached child whose parent never reaps stays defunct until the parent
	// exits). The old Process.Release() path never called Wait, so this fired
	// never.
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		t.Fatal("spawn child was never reaped (Wait not called)")
	}
}

func TestSpawnRequiresToken(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/spawn?cmd=exit%200", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code = %d, want 403", rec.Code)
	}
}

func TestPutWritesFileWithinBase(t *testing.T) {
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	req := newClientRequest("POST", "/put?token=secret&path=sub/up.txt", strings.NewReader("HELLO"))
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
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/put?token=secret&path=x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /put: code=%d, want 405", rec.Code)
	}
}

func TestPutBlocksTraversal(t *testing.T) {
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	req := newClientRequest("POST", "/put?token=secret&path=../escape.txt", strings.NewReader("x"))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("traversal must not write (code=%d)", rec.Code)
	}
}

func TestGetBlocksSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A symlink inside base pointing at a directory outside it. A lexical
	// prefix check on base/link/secret.txt passes, but following the link
	// escapes base — safeJoin must refuse it.
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/get?token=secret&path=link/secret.txt", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("symlink escape must not return 200 (code=%d body=%q)", rec.Code, rec.Body.String())
	}
}

func TestPutBlocksSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	req := newClientRequest("POST", "/put?token=secret&path=link/evil.txt", strings.NewReader("x"))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("symlink escape write must not succeed (code=%d)", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.txt")); err == nil {
		t.Fatalf("file was written outside base through a symlink")
	}
}

func TestReadBlocksDirectory(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base, ReadGlobs: []string{"sub", "*"}}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/read/sub?token=secret", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("directory must not be served (code=%d)", rec.Code)
	}
}

func TestGetBlocksDirectory(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/get?token=secret&path=sub", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("directory must not be served via /get (code=%d)", rec.Code)
	}
}

func TestLogsAliasDeniesNonMatchingGlob(t *testing.T) {
	s, base := newTestServer(t)
	if err := os.WriteFile(filepath.Join(base, "passwd"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/logs/passwd?token=secret", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("/logs/ alias must also deny non-matching globs (code=%d)", rec.Code)
	}
}

// errCapturer is a Capturer that always fails with err.
type errCapturer struct{ err error }

func (e errCapturer) Capture() ([]byte, error) { return nil, e.err }

func TestScreenshotNoInteractiveSessionReturns409(t *testing.T) {
	s := &Server{Token: "secret", Capturer: errCapturer{err: ErrNoInteractiveSession}}
	rec := httptest.NewRecorder()
	req := newClientRequest(http.MethodGet, "/screenshot?token=secret", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got status %d, want 409", rec.Code)
	}
}

func TestScreenshotOtherErrorReturns500(t *testing.T) {
	s := &Server{Token: "secret", Capturer: errCapturer{err: errors.New("boom")}}
	rec := httptest.NewRecorder()
	req := newClientRequest(http.MethodGet, "/screenshot?token=secret", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want 500", rec.Code)
	}
}
