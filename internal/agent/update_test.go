package agent

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
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

	"github.com/pddstudio/vmrc/internal/selfupdate"
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

// fakeRelease is a one-tag fake GitHub release server plus the public half of
// the (test-time generated) key that signed its checksums.txt.
type fakeRelease struct {
	srv *httptest.Server
	pub ed25519.PublicKey
}

// updateConfig returns an UpdateConfig pointed at the fake server that trusts
// the fake release's signing key.
func (f fakeRelease) updateConfig() UpdateConfig {
	return UpdateConfig{
		Repo: "o/r", BinaryName: "vmrc-agent", Version: "v0.1.0",
		APIBase: f.srv.URL, trustedKeys: []ed25519.PublicKey{f.pub},
	}
}

// fakeReleaseServer serves the releases API plus asset, checksum and checksum
// signature downloads for one tag, all from one httptest.Server. With
// signed=false the release has no checksums.txt.sig asset.
func fakeReleaseServer(t *testing.T, tag string, archive []byte, signed bool) fakeRelease {
	t.Helper()
	asset := goreleaserAsset("vmrc-agent", tag)
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig := selfupdate.SignChecksums(priv, []byte(checksums))

	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"), strings.Contains(r.URL.Path, "/releases/tags/"):
			assets := []map[string]string{
				{"name": asset, "browser_download_url": srv.URL + "/dl/" + asset},
				{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums.txt"},
			}
			if signed {
				assets = append(assets, map[string]string{"name": "checksums.txt.sig", "browser_download_url": srv.URL + "/dl/checksums.txt.sig"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":   tag,
				"name":       tag,
				"body":       "notes",
				"prerelease": false,
				"assets":     assets,
			})
		case strings.HasPrefix(r.URL.Path, "/dl/checksums.txt.sig"):
			_, _ = w.Write(sig)
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
	return fakeRelease{srv: srv, pub: pub}
}

func TestUpdateNotConfigured(t *testing.T) {
	s, _ := newTestServer(t) // Update.Repo == ""
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/update?check=1&token=secret", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("not configured: code=%d, want 501", rec.Code)
	}
}

func TestUpdateRequiresToken(t *testing.T) {
	s, _ := newTestServer(t)
	s.Update = UpdateConfig{Repo: "o/r", BinaryName: "vmrc-agent", Version: "v0.1.0"}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/update?check=1", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token: code=%d, want 403", rec.Code)
	}
}

func TestUpdateCheck(t *testing.T) {
	rel := fakeReleaseServer(t, "v0.2.0", makeAgentArchive(t, []byte("new")), true)
	s, _ := newTestServer(t)
	s.Update = rel.updateConfig()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/update?check=1&token=secret", nil))
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
	rel := fakeReleaseServer(t, "v0.2.0", makeAgentArchive(t, []byte("NEW-AGENT-BINARY")), true)
	s, _ := newTestServer(t)
	exe := filepath.Join(t.TempDir(), "vmrc-agent")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	exitCh := make(chan int, 1)
	s.Update = rel.updateConfig()
	s.execPathFn = func() (string, error) { return exe, nil }
	s.ExitFunc = func(code int) { exitCh <- code }
	s.RestartDelay = time.Millisecond

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/update?token=secret", nil))
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

// TestUpdateRefusesUnverifiedRelease: the remote /update path goes through the
// same signature check as the CLI. An unsigned release, or one signed by a key
// the agent does not trust, must be refused without touching the binary or
// scheduling a restart.
func TestUpdateRefusesUnverifiedRelease(t *testing.T) {
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		signed bool
		keys   func(fakeRelease) []ed25519.PublicKey
	}{
		{"unsigned release", false, func(f fakeRelease) []ed25519.PublicKey { return []ed25519.PublicKey{f.pub} }},
		{"untrusted signing key", true, func(fakeRelease) []ed25519.PublicKey { return []ed25519.PublicKey{otherPub} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rel := fakeReleaseServer(t, "v0.2.0", makeAgentArchive(t, []byte("EVIL")), tc.signed)
			s, _ := newTestServer(t)
			exe := filepath.Join(t.TempDir(), "vmrc-agent")
			if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
				t.Fatal(err)
			}
			exitCh := make(chan int, 1)
			s.Update = rel.updateConfig()
			s.Update.trustedKeys = tc.keys(rel)
			s.execPathFn = func() (string, error) { return exe, nil }
			s.ExitFunc = func(code int) { exitCh <- code }
			s.RestartDelay = time.Millisecond

			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/update?token=secret", nil))
			if rec.Code == http.StatusOK {
				t.Fatalf("code=200, want refusal; body=%s", rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "signature") {
				t.Errorf("body = %q, want a signature error", rec.Body.String())
			}
			if got, _ := os.ReadFile(exe); string(got) != "OLD" {
				t.Fatalf("binary = %q, want unchanged", got)
			}
			select {
			case <-exitCh:
				t.Fatal("restart scheduled after a refused update")
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}
