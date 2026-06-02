// Package selfupdate checks GitHub Releases for a newer build of a vmrc binary,
// downloads and verifies it against the release checksums, and atomically
// replaces the running executable. It is a leaf utility: it imports nothing from
// the rest of vmrc. Both binaries (vmrc, vmrc-agent) build a Target from their
// own ldflags version vars and call Check then Apply.
//
// Everything works unauthenticated against a public repo; Target.Token is an
// optional GITHUB_TOKEN that only raises the API rate limit (and would reach a
// private repo). The package never sends an Authorization header unless Token is
// set.
package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// Sentinel errors callers can match with errors.Is.
var (
	ErrUpToDate  = errors.New("already up to date")
	ErrNoAsset   = errors.New("no matching release asset for this binary/os/arch")
	ErrChecksum  = errors.New("checksum mismatch")
	ErrNoRelease = errors.New("no release found")
)

const (
	// defaultHTTPTimeout is generous: Apply downloads ~40 MB release archives.
	defaultHTTPTimeout = 5 * time.Minute
	maxArchiveSize     = 250 << 20 // 250 MiB
	maxChecksumSize    = 1 << 20   // 1 MiB
)

// Target identifies the running binary so the package can pick the matching
// release asset and know what to replace. Each main package fills this from its
// own (unexported, per-main) version var.
type Target struct {
	Repo       string       // "pddstudio/vmrc"
	BinaryName string       // "vmrc" or "vmrc-agent"
	Version    string       // current build version, e.g. "v0.1.0" or "dev"
	GOOS       string       // defaults to runtime.GOOS when empty
	GOARCH     string       // defaults to runtime.GOARCH when empty
	HTTPClient *http.Client // optional; defaults to a client with a sane timeout
	Token      string       // optional GITHUB_TOKEN (rate limits / private repo)
	APIBase    string       // optional GitHub API root override (defaults to api.github.com)
}

func (t Target) goos() string {
	if t.GOOS != "" {
		return t.GOOS
	}
	return runtime.GOOS
}

func (t Target) goarch() string {
	if t.GOARCH != "" {
		return t.GOARCH
	}
	return runtime.GOARCH
}

func (t Target) client() *http.Client {
	if t.HTTPClient != nil {
		return t.HTTPClient
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

// Release is a resolved candidate plus the chosen asset for a Target.
type Release struct {
	TagName     string // "v0.1.1"
	Version     string // normalized, no leading v ("0.1.1")
	Prerelease  bool
	Notes       string // release body, for --check display
	AssetName   string // "vmrc_v0.1.1_linux_amd64.tar.gz"
	AssetURL    string // browser_download_url of the archive
	ChecksumURL string // browser_download_url of checksums.txt
}

// Options modifies Check behavior.
type Options struct {
	PinnedTag       string // fetch this exact tag instead of latest (allows pin/reinstall/downgrade)
	AllowPrerelease bool   // accept a prerelease as the latest candidate
}

// Check queries GitHub for the candidate release for t and resolves the asset for
// this binary/os/arch. With no PinnedTag it returns ErrUpToDate when the latest
// release is not newer than t.Version. With a PinnedTag it returns that tag's
// release regardless of version, so callers can pin, reinstall, or downgrade.
func Check(ctx context.Context, t Target, opt Options) (Release, error) {
	if t.Repo == "" || t.BinaryName == "" {
		return Release{}, fmt.Errorf("selfupdate: Repo and BinaryName are required")
	}
	var (
		gh  ghRelease
		err error
	)
	switch {
	case opt.PinnedTag != "":
		gh, err = t.fetchTag(ctx, opt.PinnedTag)
	case opt.AllowPrerelease:
		gh, err = t.fetchNewestIncludingPre(ctx)
	default:
		gh, err = t.fetchLatest(ctx)
	}
	if err != nil {
		return Release{}, err
	}
	if opt.PinnedTag == "" && !isNewer(gh.TagName, t.Version) {
		return Release{}, ErrUpToDate
	}
	return t.resolveAssets(gh)
}

// resolveAssets picks the archive asset for this target plus the checksums.txt
// asset. The exact goreleaser name is preferred; a prefix+suffix match is a
// fallback robust to any leading-v discrepancy in the version token.
func (t Target) resolveAssets(gh ghRelease) (Release, error) {
	rel := Release{
		TagName:    gh.TagName,
		Version:    normalizeVersion(gh.TagName),
		Prerelease: gh.Prerelease,
		Notes:      gh.Body,
	}
	want := assetName(t.BinaryName, gh.TagName, t.goos(), t.goarch())
	prefix := t.BinaryName + "_"
	suffix := "_" + t.goos() + "_" + t.goarch() + archiveExt(t.goos())
	for _, a := range gh.Assets {
		switch {
		case a.Name == want:
			rel.AssetName, rel.AssetURL = a.Name, a.DownloadURL
		case rel.AssetURL == "" && strings.HasPrefix(a.Name, prefix) && strings.HasSuffix(a.Name, suffix):
			rel.AssetName, rel.AssetURL = a.Name, a.DownloadURL
		}
		if a.Name == "checksums.txt" {
			rel.ChecksumURL = a.DownloadURL
		}
	}
	if rel.AssetURL == "" {
		return Release{}, fmt.Errorf("%w: %s", ErrNoAsset, want)
	}
	if rel.ChecksumURL == "" {
		return Release{}, fmt.Errorf("release %s has no checksums.txt; cannot verify download", gh.TagName)
	}
	return rel, nil
}

// GateDevBuild returns an error when version is a dev/source build (not a
// release version) and the caller has not explicitly opted in via force or a
// pinned tag, so a source build is never silently overwritten by a release
// binary. Both commands call it before Apply.
func GateDevBuild(version string, force bool, pinnedTag string) error {
	if isReleaseVersion(normalizeVersion(version)) || force || pinnedTag != "" {
		return nil
	}
	return fmt.Errorf("this is a dev build (%s); re-run with --force or --version <tag> to install a release", version)
}

// Apply downloads rel's asset, verifies its SHA-256 against checksums.txt BEFORE
// extraction, extracts the binary, and atomically replaces the executable at
// execPath (the caller passes os.Executable()). On any failure before the swap
// the on-disk binary is untouched; a failed swap is rolled back by the
// underlying replace.
func Apply(ctx context.Context, t Target, rel Release, execPath string) error {
	if rel.AssetURL == "" || rel.ChecksumURL == "" {
		return fmt.Errorf("selfupdate: incomplete release (call Check first)")
	}
	sumsBytes, err := t.download(ctx, rel.ChecksumURL, maxChecksumSize)
	if err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	sums, err := parseChecksums(bytes.NewReader(sumsBytes))
	if err != nil {
		return fmt.Errorf("parsing checksums: %w", err)
	}
	want, ok := sums[rel.AssetName]
	if !ok {
		return fmt.Errorf("%w: %s not listed in checksums.txt", ErrChecksum, rel.AssetName)
	}
	archive, err := t.download(ctx, rel.AssetURL, maxArchiveSize)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", rel.AssetName, err)
	}
	if err := verifySHA256(archive, want); err != nil {
		return err
	}
	bin, err := extractBinary(archive, rel.AssetName, t.BinaryName)
	if err != nil {
		return err
	}
	if err := applyBinary(bin, execPath); err != nil {
		return fmt.Errorf("replacing %s: %w", execPath, err)
	}
	return nil
}
