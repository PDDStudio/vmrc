package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
)

func TestSetupRelocatesBinaryIntoBinDir(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, binaryFileName())
	if err := os.WriteFile(src, []byte("AGENT"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(t.TempDir(), "bin")
	dst, err := relocateRunningBinary(src, binDir)
	if err != nil {
		t.Fatalf("relocateRunningBinary: %v", err)
	}
	if filepath.Dir(dst) != binDir {
		t.Fatalf("binary not placed in bin dir: %q", dst)
	}
	if b, _ := os.ReadFile(dst); string(b) != "AGENT" {
		t.Fatalf("content mismatch")
	}
}

func TestNeedEnroll(t *testing.T) {
	// authkey present → always enroll (refresh identity / first run)
	if !needEnroll(false, "tskey-abc") {
		t.Error("authkey set, no state → should enroll")
	}
	if !needEnroll(true, "tskey-abc") {
		t.Error("authkey set, state present → should still enroll")
	}
	// no authkey: enroll only if there is no state to resume
	if !needEnroll(false, "") {
		t.Error("no authkey, no state → should enroll (will fail fast, but is attempted)")
	}
	if needEnroll(true, "") {
		t.Error("no authkey, state present → should skip enroll (resume)")
	}
}

func TestShouldWriteConfig(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "agent.json")
	if !shouldWriteConfig(missing) {
		t.Error("missing config should be written")
	}
	if err := os.WriteFile(missing, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if shouldWriteConfig(missing) {
		t.Error("existing config must not be overwritten")
	}
}

func TestCheckSetupAuth(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
		ok   bool
	}{
		{"nothing configured", config.Config{}, false},
		{"env token only (not persisted)", config.Config{Token: "t"}, false},
		{"login allowlist", config.Config{AllowedLogins: []string{"op@example.com"}}, true},
		{"tag allowlist", config.Config{AllowedTags: []string{"tag:ops"}}, true},
		{"token file", config.Config{TokenFile: "/etc/vmrc/token"}, true},
	}
	for _, tc := range cases {
		if err := checkSetupAuth(tc.cfg); (err == nil) != tc.ok {
			t.Errorf("%s: err=%v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}
