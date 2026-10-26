package agent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newUploadsServer returns a server whose /put root is a separate uploads dir
// (here, the default layout <base>/uploads).
func newUploadsServer(t *testing.T) (s *Server, base, uploads string) {
	t.Helper()
	base = t.TempDir()
	uploads = filepath.Join(base, "uploads")
	if err := os.MkdirAll(uploads, 0o750); err != nil {
		t.Fatal(err)
	}
	return &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base, UploadsDir: uploads}, base, uploads
}

func doPut(s *Server, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := newClientRequest("PUT", "/put?token=secret&path="+path, strings.NewReader(body))
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPutResolvesRelativeToUploadsDir(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string // relative to the uploads dir
	}{
		{"top level", "up.txt", "up.txt"},
		{"nested", "a/b/c.txt", filepath.Join("a", "b", "c.txt")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, base, uploads := newUploadsServer(t)
			rec := doPut(s, tt.path, "HELLO")
			if rec.Code != http.StatusOK {
				t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
			}
			if got, err := os.ReadFile(filepath.Join(uploads, tt.want)); err != nil || string(got) != "HELLO" {
				t.Fatalf("file under uploads = %q err=%v", got, err)
			}
			if _, err := os.Stat(filepath.Join(base, tt.want)); err == nil {
				t.Fatalf("file must not land directly in base_dir")
			}
		})
	}
}

func TestPutUploadIsReadableViaGetUnderUploadsPrefix(t *testing.T) {
	s, _, _ := newUploadsServer(t)
	if rec := doPut(s, "x/y.txt", "DATA"); rec.Code != http.StatusOK {
		t.Fatalf("put code=%d", rec.Code)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/get?token=secret&path=uploads/x/y.txt", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "DATA" {
		t.Fatalf("get uploads/x/y.txt: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestPutRefusesEscapingUploadsDir(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		symlink bool // create uploads/link -> <outside>
	}{
		{name: "parent traversal into base", path: "../escape.txt"},
		{name: "deep traversal", path: "a/../../../escape.txt"},
		{name: "encoded traversal", path: "..%2fescape.txt"},
		{name: "symlink escape", path: "link/evil.txt", symlink: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, base, uploads := newUploadsServer(t)
			outside := t.TempDir()
			if tt.symlink {
				if err := os.Symlink(outside, filepath.Join(uploads, "link")); err != nil {
					t.Skipf("symlinks unsupported here: %v", err)
				}
			}
			rec := doPut(s, tt.path, "x")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("code=%d, want 403 (body=%q)", rec.Code, rec.Body.String())
			}
			for _, p := range []string{filepath.Join(base, "escape.txt"), filepath.Join(outside, "evil.txt")} {
				if _, err := os.Stat(p); err == nil {
					t.Fatalf("file written outside uploads dir: %s", p)
				}
			}
		})
	}
}

func TestPutRecreatesMissingUploadsDir(t *testing.T) {
	base := t.TempDir()
	uploads := filepath.Join(base, "uploads") // deliberately not created
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base, UploadsDir: uploads}
	if rec := doPut(s, "f.txt", "X"); rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(uploads, "f.txt")); err != nil {
		t.Fatalf("upload missing: %v", err)
	}
}

func TestPutWithoutUploadsDirFallsBackToBase(t *testing.T) {
	// A Server constructed without UploadsDir (library use, the lifted tests)
	// keeps the historical behavior of writing relative to the base dir.
	base := t.TempDir()
	s := &Server{Token: "secret", Capturer: fakeCapturer{}, BaseDir: base}
	if rec := doPut(s, "f.txt", "X"); rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(base, "f.txt")); err != nil {
		t.Fatalf("fallback upload missing: %v", err)
	}
}
