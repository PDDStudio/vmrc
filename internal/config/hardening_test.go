package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func validBase(t *testing.T) Config {
	t.Helper()
	c := Defaults()
	dir := t.TempDir()
	c.StateDir, c.BaseDir, c.BinDir = filepath.Join(dir, "state"), filepath.Join(dir, "files"), filepath.Join(dir, "bin")
	c.ReadGlobs = []string{"*.log"}
	c.AllowedLogins = []string{"op@example.com"}
	return c
}

func hasWarning(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func TestLoadFileRejectsUnknownKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	// A typo'd allowlist key used to be ignored, silently yielding deny-all.
	if err := os.WriteFile(p, []byte(`{"allowed_login": ["op@example.com"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(Defaults(), p)
	if err == nil || !strings.Contains(err.Error(), "allowed_login") {
		t.Fatalf("unknown key must be an error naming it, got %v", err)
	}
}

func TestValidateWarnsWhenNothingCanAuthorize(t *testing.T) {
	c := validBase(t)
	c.AllowedLogins = nil
	ws, err := Validate(c)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(ws, "deny every request") {
		t.Fatalf("no allowlist and no token must warn, got %v", ws)
	}
	c.Token = "t"
	if ws, _ := Validate(c); hasWarning(ws, "deny every request") {
		t.Fatalf("a token is enough: %v", ws)
	}
}

func TestValidateWarnsWhenAgentTagIsAllowed(t *testing.T) {
	c := validBase(t)
	c.AllowedTags = []string{"tag:ops", "tag:vmrc"}
	ws, _ := Validate(c)
	if !hasWarning(ws, "tag:vmrc") {
		t.Fatalf("allowing the agents' own tag lets every agent drive every other; want a warning, got %v", ws)
	}
}

func TestValidateRejectsDangerousUploadsDir(t *testing.T) {
	c := validBase(t)
	home, _ := os.UserHomeDir()
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	for _, dir := range []string{root, home, c.BaseDir, filepath.Dir(c.BaseDir)} {
		c.UploadsDir = dir
		if _, err := Validate(c); err == nil {
			t.Errorf("uploads_dir %q must be refused while retention is on (the sweeper would expire everything under it)", dir)
		}
	}
	c.UploadsDir = filepath.Join(c.BaseDir, "incoming")
	if _, err := Validate(c); err != nil {
		t.Fatalf("a dedicated dir is fine: %v", err)
	}
	c.UploadsDir, c.UploadRetention = home, 0
	if _, err := Validate(c); err != nil {
		t.Fatalf("with retention off nothing is deleted, so any dir is allowed: %v", err)
	}
}

func TestResolveTokenFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "token")
	if err := os.WriteFile(p, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Config{TokenFile: p}
	if err := c.ResolveToken(); err != nil || c.Token != "s3cret" {
		t.Fatalf("token=%q err=%v", c.Token, err)
	}
	c = Config{TokenFile: p, Token: "from-env"}
	if err := c.ResolveToken(); err != nil || c.Token != "from-env" {
		t.Fatalf("env/flag token must win over token_file: token=%q err=%v", c.Token, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
		c = Config{TokenFile: p}
		if err := c.ResolveToken(); err == nil {
			t.Fatal("a group/world-readable token file must be refused")
		}
	}
	c = Config{TokenFile: filepath.Join(dir, "missing")}
	if err := c.ResolveToken(); err == nil {
		t.Fatal("a missing token file must be an error, not a silent deny-all")
	}
}

func TestExampleConfigLoads(t *testing.T) {
	// The documented example must stay loadable under strict decoding: every key
	// in it has to be a real config key.
	if _, err := LoadFile(Defaults(), filepath.Join("..", "..", "vmrc.example.json")); err != nil {
		t.Fatalf("vmrc.example.json: %v", err)
	}
}
