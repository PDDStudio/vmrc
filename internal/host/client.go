package host

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client is a per-agent HTTP verb client. BaseURL is like
// "http://vm.tailnet.ts.net:47800". Token, when non-empty, is sent as ?token=.
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

func (c *Client) url(endpoint string, q url.Values) string {
	if c.Token != "" {
		q.Set("token", c.Token)
	}
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
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, -1, err
	}
	defer resp.Body.Close()
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
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, errFromResponse(resp)
	}
	return io.ReadAll(resp.Body)
}

// Get fetches remotePath via /get. The caller must close the returned reader.
func (c *Client) Get(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	q := url.Values{}
	q.Set("path", remotePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/get", q), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		return nil, errFromResponse(resp)
	}
	return resp.Body, nil
}

// Put uploads body to remotePath via POST /put.
func (c *Client) Put(ctx context.Context, remotePath string, body io.Reader) error {
	q := url.Values{}
	q.Set("path", remotePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/put", q), body)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return errFromResponse(resp)
	}
	return nil
}
