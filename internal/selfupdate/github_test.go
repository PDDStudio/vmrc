package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGitHub serves a single repo's releases API plus its asset + checksum
// downloads, all from one httptest.Server. It overrides apiBase for the test.
type fakeGitHub struct {
	srv        *httptest.Server
	tag        string
	prerelease bool
	assetName  string
	archive    []byte
	checksums  string // checksums.txt body (may be empty to omit the asset)
	omitAsset  bool   // serve a release whose assets lack the target archive
	notFound   bool   // /releases/latest returns 404

	// Release signing. newFakeGitHub generates signKey (and trusts its public
	// half via Target.TrustedKeys) unless one is set. The checksums.txt.sig body
	// is computed at serve time over the CURRENT checksums, so tests that swap
	// checksums still get a valid signature unless they set sig explicitly.
	signKey ed25519.PrivateKey
	sig     []byte // fixed checksums.txt.sig body; nil = sign current checksums
	omitSig bool   // serve a release without a checksums.txt.sig asset
}

// newFakeGitHub builds the server for binary "vmrc" on linux/amd64.
func newFakeGitHub(t *testing.T, f *fakeGitHub) Target {
	t.Helper()
	f.tag = orDefault(f.tag, "v0.2.0")
	f.assetName = assetName("vmrc", f.tag, "linux", "amd64")
	if f.archive == nil {
		f.archive = makeTarGz(t, archiveEntry{"vmrc", []byte("new-vmrc-binary-" + f.tag)})
	}
	if f.checksums == "" && !f.omitAsset {
		sum := sha256.Sum256(f.archive)
		f.checksums = fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), f.assetName)
	}

	if f.signKey == nil {
		_, f.signKey = genKey(t)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			if f.notFound {
				http.Error(w, "Not Found", http.StatusNotFound)
				return
			}
			writeJSON(w, f.release())
		case strings.Contains(r.URL.Path, "/releases/tags/"):
			writeJSON(w, f.release())
		case strings.HasSuffix(r.URL.Path, "/releases"):
			writeJSON(w, []ghRelease{f.release()})
		case strings.HasPrefix(r.URL.Path, "/dl/checksums.txt.sig"):
			if f.sig != nil {
				_, _ = w.Write(f.sig)
				return
			}
			_, _ = w.Write(SignChecksums(f.signKey, []byte(f.checksums)))
		case strings.HasPrefix(r.URL.Path, "/dl/checksums.txt"):
			_, _ = w.Write([]byte(f.checksums))
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			_, _ = w.Write(f.archive)
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusInternalServerError)
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	return Target{
		Repo:        "o/r",
		BinaryName:  "vmrc",
		Version:     "v0.1.0",
		GOOS:        "linux",
		GOARCH:      "amd64",
		HTTPClient:  f.srv.Client(),
		APIBase:     f.srv.URL,
		TrustedKeys: []ed25519.PublicKey{f.signKey.Public().(ed25519.PublicKey)},
	}
}

func (f *fakeGitHub) release() ghRelease {
	rel := ghRelease{TagName: f.tag, Name: f.tag, Body: "notes", Prerelease: f.prerelease}
	if f.checksums != "" {
		rel.Assets = append(rel.Assets, ghAsset{Name: "checksums.txt", DownloadURL: f.srv.URL + "/dl/checksums.txt"})
		if !f.omitSig {
			rel.Assets = append(rel.Assets, ghAsset{Name: "checksums.txt.sig", DownloadURL: f.srv.URL + "/dl/checksums.txt.sig"})
		}
	}
	if !f.omitAsset {
		rel.Assets = append(rel.Assets, ghAsset{Name: f.assetName, DownloadURL: f.srv.URL + "/dl/" + f.assetName})
	}
	return rel
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func TestCheck(t *testing.T) {
	t.Run("newer release available", func(t *testing.T) {
		tgt := newFakeGitHub(t, &fakeGitHub{tag: "v0.2.0"})
		rel, err := Check(context.Background(), tgt, Options{})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if rel.TagName != "v0.2.0" || rel.AssetName != "vmrc_v0.2.0_linux_amd64.tar.gz" {
			t.Errorf("got tag=%q asset=%q", rel.TagName, rel.AssetName)
		}
		if rel.ChecksumURL == "" {
			t.Error("ChecksumURL empty")
		}
		if rel.SignatureURL == "" {
			t.Error("SignatureURL empty")
		}
	})

	t.Run("up to date", func(t *testing.T) {
		tgt := newFakeGitHub(t, &fakeGitHub{tag: "v0.1.0"}) // == current
		_, err := Check(context.Background(), tgt, Options{})
		if !errors.Is(err, ErrUpToDate) {
			t.Errorf("Check = %v, want ErrUpToDate", err)
		}
	})

	t.Run("pinned tag returns even if not newer", func(t *testing.T) {
		tgt := newFakeGitHub(t, &fakeGitHub{tag: "v0.0.9"}) // older than current
		rel, err := Check(context.Background(), tgt, Options{PinnedTag: "v0.0.9"})
		if err != nil {
			t.Fatalf("Check(pinned): %v", err)
		}
		if rel.TagName != "v0.0.9" {
			t.Errorf("got %q, want v0.0.9", rel.TagName)
		}
	})

	t.Run("missing asset", func(t *testing.T) {
		tgt := newFakeGitHub(t, &fakeGitHub{tag: "v0.2.0", omitAsset: true})
		_, err := Check(context.Background(), tgt, Options{})
		if !errors.Is(err, ErrNoAsset) {
			t.Errorf("Check = %v, want ErrNoAsset", err)
		}
	})

	t.Run("missing signature asset is refused", func(t *testing.T) {
		tgt := newFakeGitHub(t, &fakeGitHub{tag: "v0.2.0", omitSig: true})
		_, err := Check(context.Background(), tgt, Options{})
		if !errors.Is(err, ErrSignature) {
			t.Errorf("Check = %v, want ErrSignature", err)
		}
	})

	t.Run("no releases (404)", func(t *testing.T) {
		tgt := newFakeGitHub(t, &fakeGitHub{notFound: true})
		_, err := Check(context.Background(), tgt, Options{})
		if !errors.Is(err, ErrNoRelease) {
			t.Errorf("Check = %v, want ErrNoRelease", err)
		}
	})

	t.Run("prerelease included with AllowPrerelease", func(t *testing.T) {
		tgt := newFakeGitHub(t, &fakeGitHub{tag: "v0.3.0-rc1", prerelease: true})
		rel, err := Check(context.Background(), tgt, Options{AllowPrerelease: true})
		if err != nil {
			t.Fatalf("Check(pre): %v", err)
		}
		if !rel.Prerelease || rel.TagName != "v0.3.0-rc1" {
			t.Errorf("got tag=%q pre=%v", rel.TagName, rel.Prerelease)
		}
	})
}
