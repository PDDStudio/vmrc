package host

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pddstudio/vmrc/internal/agent"
)

type pngCapturer struct{}

func (pngCapturer) Capture() ([]byte, error) { return []byte("PNG"), nil }

type nopInjector struct{}

func (nopInjector) Click(x, y int, button string, double bool) error { return nil }
func (nopInjector) Type(text string) error                           { return nil }
func (nopInjector) Key(combo string) error                           { return nil }

// TestClientAgainstRealAgent drives the real agent handler with the real host
// client, so a change to the request contract on either side (methods, the
// client header, auth) fails here rather than in the field.
func TestClientAgainstRealAgent(t *testing.T) {
	s := &agent.Server{Token: "tok", BaseDir: t.TempDir(), Capturer: pngCapturer{}, Injector: nopInjector{}}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	c := newClient(srv, "tok")
	ctx := context.Background()

	out, status, err := c.Exec(ctx, "echo hi")
	if err != nil || status != 0 || strings.TrimSpace(string(out)) != "hi" {
		t.Fatalf("Exec: out=%q status=%d err=%v", out, status, err)
	}
	if err := c.Put(ctx, "f.txt", bytes.NewReader([]byte("data"))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, err := c.Get(ctx, "f.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != "data" {
		t.Fatalf("Get = %q", got)
	}
	if png, err := c.Screenshot(ctx); err != nil || string(png) != "PNG" {
		t.Fatalf("Screenshot: %q %v", png, err)
	}
	if err := c.Click(ctx, 1, 2, "", false); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if err := c.Type(ctx, "x"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if err := c.Key(ctx, "Enter"); err != nil {
		t.Fatalf("Key: %v", err)
	}
}
