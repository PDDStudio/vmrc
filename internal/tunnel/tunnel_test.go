package tunnel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectScheme(t *testing.T) {
	tests := []struct {
		name        string
		certDomains []string
		fqdn        string
		wantScheme  string
		wantReason  bool // true => reason must be non-empty
	}{
		{"https when fqdn is a cert domain", []string{"vm1.tail-scale.ts.net"}, "vm1.tail-scale.ts.net", "https", false},
		{"http when fqdn absent", []string{"other.ts.net"}, "vm1.tail-scale.ts.net", "http", true},
		{"http when no cert domains", nil, "vm1.tail-scale.ts.net", "http", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, reason := selectScheme(tt.certDomains, tt.fqdn)
			if scheme != tt.wantScheme {
				t.Errorf("scheme = %q, want %q", scheme, tt.wantScheme)
			}
			if (reason != "") != tt.wantReason {
				t.Errorf("reason = %q, want non-empty=%v", reason, tt.wantReason)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		stateExists bool
		wantErr     bool
	}{
		{"authkey present, no state", Config{AuthKey: "tskey-abc"}, false, false},
		{"no authkey but state exists", Config{StateDir: "/tmp/x"}, true, false},
		{"no authkey and no state", Config{StateDir: "/tmp/x"}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg, tt.stateExists)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHasState(t *testing.T) {
	if hasState("") {
		t.Error(`hasState("") = true, want false`)
	}
	dir := t.TempDir()
	if hasState(dir) {
		t.Error("empty dir reported as having state")
	}
	if err := os.WriteFile(filepath.Join(dir, "tailscaled.state"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasState(dir) {
		t.Error("state file present but hasState = false")
	}
}
