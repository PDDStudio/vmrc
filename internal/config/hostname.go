package config

import (
	"os"
	"strings"
)

// fallbackHostname is the tsnet node name used when the OS hostname yields
// nothing usable as a DNS label.
const fallbackHostname = "vmrc-agent"

// sanitizeHostname turns a raw OS hostname into a single valid DNS label:
// trimmed and lowercased, first label only (drops a trailing ".local" or any
// domain), every character outside [a-z0-9-] replaced with '-', leading and
// trailing '-' trimmed, truncated to 63 chars. Returns fallbackHostname if
// nothing usable remains. Pure (no I/O) so it is fully unit-testable.
func sanitizeHostname(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i] // keep only the first DNS label
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-")
	}
	if out == "" {
		return fallbackHostname
	}
	return out
}

// DefaultHostname returns a stable, DNS-valid tsnet node name derived from the
// machine's OS hostname, or fallbackHostname when the hostname is unavailable
// or unusable. It is the default node name when none is configured, ensuring
// the node name, MagicDNS name, and issued cert always agree.
func DefaultHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return fallbackHostname
	}
	return sanitizeHostname(h)
}
