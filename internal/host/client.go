package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client is a per-agent HTTP verb client. BaseURL is like
// "http://vm.tailnet.ts.net:47800". Token, when non-empty, is sent as an
// "Authorization: Bearer <token>" header (kept out of URLs, logs, and proxies).
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Token   string
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// do sets the bearer token (when configured) and dispatches the request.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return c.httpClient().Do(req)
}

func (c *Client) url(endpoint string, q url.Values) string {
	u := c.BaseURL + endpoint
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	return u
}

// errFromResponse turns a non-2xx response into an error with a body snippet.
func errFromResponse(resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("agent returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
}

// Exec runs cmdline via /exec and returns combined output plus the command's
// exit status (from X-Exit-Status). A non-zero exit status is NOT an error.
func (c *Client) Exec(ctx context.Context, cmdline string) ([]byte, int, error) {
	q := url.Values{}
	q.Set("cmd", cmdline)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/exec", q), nil)
	if err != nil {
		return nil, -1, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, -1, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, -1, errFromResponse(resp)
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, -1, err
	}
	status := 0
	if h := resp.Header.Get("X-Exit-Status"); h != "" {
		status, _ = strconv.Atoi(h)
	}
	return out, status, nil
}

// Screenshot fetches a PNG via /screenshot.
func (c *Client) Screenshot(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/screenshot", url.Values{}), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, errFromResponse(resp)
	}
	return io.ReadAll(resp.Body)
}

// Click injects a mouse click at absolute screen pixel (x,y) via POST /click.
// button is "left", "right", or "middle" (empty means left); double issues a
// double-click.
func (c *Client) Click(ctx context.Context, x, y int, button string, double bool) error {
	q := url.Values{}
	q.Set("x", strconv.Itoa(x))
	q.Set("y", strconv.Itoa(y))
	if button != "" {
		q.Set("button", button)
	}
	if double {
		q.Set("double", "1")
	}
	return c.post(ctx, "/click", q)
}

// Type enters text as literal Unicode keystrokes via POST /type.
func (c *Client) Type(ctx context.Context, text string) error {
	q := url.Values{}
	q.Set("text", text)
	return c.post(ctx, "/type", q)
}

// Key presses a key combo (e.g. "Enter", "Ctrl+C") via POST /key.
func (c *Client) Key(ctx context.Context, combo string) error {
	q := url.Values{}
	q.Set("combo", combo)
	return c.post(ctx, "/key", q)
}

// post issues a POST to endpoint with q as the query string and no body,
// treating any non-2xx reply as an error. Used by the fire-and-forget input
// verbs whose response body carries nothing the caller needs.
func (c *Client) post(ctx context.Context, endpoint string, q url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(endpoint, q), nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return errFromResponse(resp)
	}
	return nil
}

// Get fetches remotePath via /get. The caller must close the returned reader.
func (c *Client) Get(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	q := url.Values{}
	q.Set("path", remotePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/get", q), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer func() { _ = resp.Body.Close() }()
		return nil, errFromResponse(resp)
	}
	return resp.Body, nil
}

// UpdateParams are the optional self-update knobs forwarded to an agent's
// /update endpoint as query parameters.
type UpdateParams struct {
	Version string // pin a release tag
	Pre     bool   // consider prereleases
	Force   bool   // allow updating a dev build
}

func (p UpdateParams) apply(q url.Values) {
	if p.Version != "" {
		q.Set("version", p.Version)
	}
	if p.Pre {
		q.Set("pre", "1")
	}
	if p.Force {
		q.Set("force", "1")
	}
}

// UpdateStatus is the agent's reply to GET /update?check=1.
type UpdateStatus struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Available  bool   `json:"available"`
	Prerelease bool   `json:"prerelease"`
	Notes      string `json:"notes,omitempty"`
}

// UpdateResult is the agent's reply to a successful POST /update.
type UpdateResult struct {
	Old        string `json:"old"`
	New        string `json:"new"`
	Restarting bool   `json:"restarting"`
}

// CheckUpdate asks the agent which release it would install, without applying.
func (c *Client) CheckUpdate(ctx context.Context, p UpdateParams) (UpdateStatus, error) {
	q := url.Values{}
	q.Set("check", "1")
	p.apply(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/update", q), nil)
	if err != nil {
		return UpdateStatus{}, err
	}
	resp, err := c.do(req)
	if err != nil {
		return UpdateStatus{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return UpdateStatus{}, errFromResponse(resp)
	}
	var st UpdateStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return UpdateStatus{}, err
	}
	return st, nil
}

// Update tells the agent to self-update and restart. The agent replies with the
// old/new versions, then drops the connection as it restarts; a clean EOF after
// a 2xx is treated as "restart initiated", not an error.
func (c *Client) Update(ctx context.Context, p UpdateParams) (UpdateResult, error) {
	q := url.Values{}
	p.apply(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/update", q), nil)
	if err != nil {
		return UpdateResult{}, err
	}
	resp, err := c.do(req)
	if err != nil {
		return UpdateResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return UpdateResult{}, errFromResponse(resp)
	}
	var res UpdateResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			// Agent restarted before the body fully flushed; the update was accepted.
			return UpdateResult{Restarting: true}, nil
		}
		return UpdateResult{}, err
	}
	return res, nil
}

// Put uploads body to remotePath via POST /put.
func (c *Client) Put(ctx context.Context, remotePath string, body io.Reader) error {
	q := url.Values{}
	q.Set("path", remotePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/put", q), body)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return errFromResponse(resp)
	}
	return nil
}
