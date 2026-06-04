package main

import (
	"slices"
	"strings"
	"testing"
)

func TestRedactArgsMasksSecrets(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "space-separated token",
			in:   []string{"run", "--token", "s3cret", "--hostname", "vm1"},
			want: []string{"run", "--token", "***", "--hostname", "vm1"},
		},
		{
			name: "equals token",
			in:   []string{"run", "--token=s3cret"},
			want: []string{"run", "--token=***"},
		},
		{
			name: "single-dash and github-token",
			in:   []string{"-token", "s3cret", "--github-token=ghp_x"},
			want: []string{"-token", "***", "--github-token=***"},
		},
		{
			name: "nothing to redact",
			in:   []string{"run", "--listen-port", "47800"},
			want: []string{"run", "--listen-port", "47800"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := redactArgs(tc.in)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("redactArgs(%v) = %v, want %v", tc.in, got, tc.want)
			}
			if joined := strings.Join(got, " "); strings.Contains(joined, "s3cret") || strings.Contains(joined, "ghp_x") {
				t.Fatalf("secret leaked through redaction: %q", joined)
			}
		})
	}
}
