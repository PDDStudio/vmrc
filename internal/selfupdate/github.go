package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// defaultAPIBase is the GitHub REST API root. A Target.APIBase override (used by
// tests and callers behind a proxy) takes precedence.
const defaultAPIBase = "https://api.github.com"

// base returns the API root for t: the explicit override, else the default.
func (t Target) base() string {
	if t.APIBase != "" {
		return t.APIBase
	}
	return defaultAPIBase
}

// ghRelease is the subset of the GitHub release JSON we consume.
type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Name       string    `json:"name"`
	Body       string    `json:"body"`
	Prerelease bool      `json:"prerelease"`
	Draft      bool      `json:"draft"`
	Assets     []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

// fetchLatest GETs /releases/latest, which GitHub defines to exclude prereleases
// and drafts.
func (t Target) fetchLatest(ctx context.Context) (ghRelease, error) {
	var rel ghRelease
	err := t.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", t.base(), t.Repo), &rel)
	return rel, err
}

// fetchTag GETs /releases/tags/{tag}.
func (t Target) fetchTag(ctx context.Context, tag string) (ghRelease, error) {
	var rel ghRelease
	err := t.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases/tags/%s", t.base(), t.Repo, tag), &rel)
	return rel, err
}

// fetchNewestIncludingPre lists releases (newest first) and returns the first
// non-draft, so a prerelease can be selected as the latest candidate.
func (t Target) fetchNewestIncludingPre(ctx context.Context) (ghRelease, error) {
	var rels []ghRelease
	if err := t.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=20", t.base(), t.Repo), &rels); err != nil {
		return ghRelease{}, err
	}
	for _, r := range rels {
		if !r.Draft {
			return r, nil
		}
	}
	return ghRelease{}, fmt.Errorf("%w: %s", ErrNoRelease, t.Repo)
}

// getJSON performs an authenticated-if-token GET and decodes the JSON body.
func (t Target) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if t.Token != "" {
		req.Header.Set("Authorization", "Bearer "+t.Token)
	}
	resp, err := t.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w (%s)", ErrNoRelease, t.Repo)
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return fmt.Errorf("github API rate limit exceeded; set GITHUB_TOKEN to raise it")
	case resp.StatusCode/100 != 2:
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("github API %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// download GETs url and returns up to max bytes. Release asset download URLs
// redirect to GitHub's CDN; the default client follows redirects.
func (t Target) download(ctx context.Context, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if t.Token != "" {
		req.Header.Set("Authorization", "Bearer "+t.Token)
	}
	resp, err := t.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("download %s: %s: %s", url, resp.Status, strings.TrimSpace(string(snippet)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, max))
}
