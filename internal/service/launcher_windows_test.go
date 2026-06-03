//go:build windows

package service

import (
	"testing"
)

func TestParseSchtasksStatus(t *testing.T) {
	cases := map[string]string{
		"Status:    Running\n": "running",
		"Status:    Ready\n":   "stopped",
		"Status:    Disabled":  "stopped",
		"":                     "unknown",
	}
	for in, want := range cases {
		if got := parseSchtasksStatus(in); got != want {
			t.Fatalf("parseSchtasksStatus(%q)=%q want %q", in, got, want)
		}
	}
}
