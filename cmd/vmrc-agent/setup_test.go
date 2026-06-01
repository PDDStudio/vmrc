package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
