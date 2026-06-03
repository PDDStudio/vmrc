package config

import (
	"strings"
	"testing"
)

func TestSanitizeHostname(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "REDACTED-HOST", "REDACTED-HOST"},
		{"uppercased", "WIN-BUILD01", "win-build01"},
		{"dot-local suffix dropped", "REDACTED-HOST.local", "REDACTED-HOST"},
		{"fqdn keeps first label", "host.example.com", "host"},
		{"underscore and space become dash", "my_host name", "my-host-name"},
		{"leading/trailing junk trimmed", "--weird!!", "weird"},
		{"truncated to 63", strings.Repeat("a", 80), strings.Repeat("a", 63)},
		{"empty falls back", "", "vmrc-agent"},
		{"only junk falls back", "!!!", "vmrc-agent"},
		{"only dots falls back", "...", "vmrc-agent"},
		{"surrounding whitespace", "  Host01  ", "host01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeHostname(tt.in); got != tt.want {
				t.Errorf("sanitizeHostname(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
