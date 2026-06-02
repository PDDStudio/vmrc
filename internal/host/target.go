// Package host is the vmrc host-side fleet client: tailnet discovery of vmrc
// agents and the HTTP verb clients that drive them. It relies on the host's
// system Tailscale for routing and identity; it embeds no tsnet node.
package host

import (
	"fmt"
	"strings"
)

// ParseTargets splits a comma-separated target list ("vm1,vm2") into agent
// names, trimming surrounding spaces. An empty spec or an empty entry is an
// error.
func ParseTargets(spec string) ([]string, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("no target specified")
	}
	parts := strings.Split(spec, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("empty target in %q", spec)
		}
		names = append(names, p)
	}
	return names, nil
}

// SplitTargetPath splits "<target>:<path>" on the FIRST colon so Windows paths
// (C:\...) in the path portion survive. Both sides must be non-empty.
func SplitTargetPath(spec string) (target, path string, err error) {
	i := strings.IndexByte(spec, ':')
	if i <= 0 || i == len(spec)-1 {
		return "", "", fmt.Errorf("expected <target>:<path>, got %q", spec)
	}
	return spec[:i], spec[i+1:], nil
}
