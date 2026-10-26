package service

import (
	"strings"
	"testing"
)

func TestSystemdUnitRestartDelayAndTarget(t *testing.T) {
	tests := []struct {
		user     bool
		wantedBy string
	}{
		{false, "WantedBy=multi-user.target"},
		{true, "WantedBy=default.target"},
	}
	for _, tt := range tests {
		u := systemdUnit(tt.user)
		if !strings.Contains(u, "}}RestartSec=5\n") {
			t.Errorf("user=%v: want RestartSec=5, got:\n%s", tt.user, u)
		}
		if strings.Contains(u, "RestartSec=120") {
			t.Errorf("user=%v: kardianos' 120s restart delay must be overridden", tt.user)
		}
		if !strings.Contains(u, tt.wantedBy) {
			t.Errorf("user=%v: want %q, got:\n%s", tt.user, tt.wantedBy, u)
		}
		if strings.Contains(u, "@") {
			t.Errorf("user=%v: unreplaced placeholder in:\n%s", tt.user, u)
		}
		// Template sanity for kardianos' engine: every block is closed.
		opens := strings.Count(u, "{{if ") + strings.Count(u, "{{range ")
		if closes := strings.Count(u, "{{end}}"); opens != closes {
			t.Errorf("user=%v: %d block opens vs %d {{end}}", tt.user, opens, closes)
		}
	}
}

func TestKardianosConfigCarriesSystemdUnit(t *testing.T) {
	k := toKardianosConfig(Config{Name: "vmrc-agent"})
	if got, _ := k.Option["SystemdScript"].(string); got != systemdUnit(false) {
		t.Fatalf("system service must use the system systemd unit, got %q", got)
	}
}
