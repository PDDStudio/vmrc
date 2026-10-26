package agent

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// goreleaserAsset builds the archive name the agent's /update handler will look
// for, for this host's GOOS/GOARCH.
func goreleaserAsset(bin, tag string) string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("%s_%s_%s_%s%s", bin, tag, runtime.GOOS, runtime.GOARCH, ext)
}

// makeAgentArchive builds an in-memory release archive containing the agent
// binary, in the format goreleaser would use for this host's GOOS.
func makeAgentArchive(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("vmrc-agent.exe")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(body)
		_ = zw.Close()
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "vmrc-agent", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// fakeReleaseServer serves the releases API plus asset + checksum downloads for
// one tag, all from one httptest.Server.
func fakeReleaseServer(t *testing.T, tag string, archive []byte) *httptest.Server {
	t.Helper()
	asset := goreleaserAsset("vmrc-agent", tag)
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)

	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"), strings.Contains(r.URL.Path, "/releases/tags/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":   tag,
				"name":       tag,
				"body":       "notes",
				"prerelease": false,
				"assets": []map[string]string{
					{"name": asset, "browser_download_url": srv.URL + "/dl/" + asset},
					{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums.txt"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/dl/checksums.txt"):
			_, _ = w.Write([]byte(checksums))
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			_, _ = w.Write(archive)
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusInternalServerError)
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestUpdateNotConfigured(t *testing.T) {
	s, _ := newTestServer(t) // Update.Repo == ""
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/update?check=1&token=secret", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("not configured: code=%d, want 501", rec.Code)
	}
}

func TestUpdateRequiresToken(t *testing.T) {
	s, _ := newTestServer(t)
	s.Update = UpdateConfig{Repo: "o/r", BinaryName: "vmrc-agent", Version: "v0.1.0"}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/update?check=1", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code=%d, want 403", rec.Code)
	}
}

func TestUpdateCheck(t *testing.T) {
	srv := fakeReleaseServer(t, "v0.2.0", makeAgentArchive(t, []byte("new")))
	s, _ := newTestServer(t)
	s.Update = UpdateConfig{Repo: "o/r", BinaryName: "vmrc-agent", Version: "v0.1.0", APIBase: srv.URL}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/update?check=1&token=secret", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var st updateStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Available || st.Latest != "v0.2.0" || st.Current != "v0.1.0" {
		t.Fatalf("status = %+v, want available v0.1.0 -> v0.2.0", st)
	}
}

func TestUpdateApplyAndRestart(t *testing.T) {
	srv := fakeReleaseServer(t, "v0.2.0", makeAgentArchive(t, []byte("NEW-AGENT-BINARY")))
	s, _ := newTestServer(t)
	exe := filepath.Join(t.TempDir(), "vmrc-agent")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	exitCh := make(chan int, 1)
	s.Update = UpdateConfig{Repo: "o/r", BinaryName: "vmrc-agent", Version: "v0.1.0", APIBase: srv.URL}
	s.execPathFn = func() (string, error) { return exe, nil }
	s.ExitFunc = func(code int) { exitCh <- code }
	s.RestartDelay = time.Millisecond

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/update?token=secret", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var res updateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Restarting || res.New != "v0.2.0" {
		t.Fatalf("result = %+v, want restarting -> v0.2.0", res)
	}
	if got, _ := os.ReadFile(exe); string(got) != "NEW-AGENT-BINARY" {
		t.Fatalf("binary = %q, want replaced", got)
	}
	select {
	case code := <-exitCh:
		if code != 1 {
			t.Fatalf("exit code = %d, want 1 (triggers supervisor restart)", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ExitFunc not called; no restart was scheduled")
	}
}
