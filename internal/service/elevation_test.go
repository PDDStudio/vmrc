package service

import (
	"errors"
	"strings"
	"testing"
)

func TestElevationHint(t *testing.T) {
	tests := []struct {
		goos     string
		contains string
	}{
		{"windows", "Administrator"},
		{"linux", "sudo"},
		{"darwin", "sudo"},
	}
	for _, tt := range tests {
		h := elevationHint(tt.goos)
		if !strings.Contains(h, tt.contains) {
			t.Errorf("elevationHint(%q) = %q, want substring %q", tt.goos, h, tt.contains)
		}
	}
}

func TestRequireElevatedOK(t *testing.T) {
	old := probeElevated
	probeElevated = func() bool { return true }
	defer func() { probeElevated = old }()
	if err := RequireElevated(); err != nil {
		t.Fatalf("elevated probe should pass, got %v", err)
	}
}

func TestRequireElevatedFails(t *testing.T) {
	old := probeElevated
	probeElevated = func() bool { return false }
	defer func() { probeElevated = old }()
	err := RequireElevated()
	var ne *NotElevatedError
	if !errors.As(err, &ne) {
		t.Fatalf("want *NotElevatedError, got %v", err)
	}
}
