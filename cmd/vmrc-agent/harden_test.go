package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
)

func TestMigrateLegacyUserConfigRewritesBinDir(t *testing.T) {
	appData := filepath.Join(t.TempDir(), "Local", "vmrc")
	protected := filepath.Join(t.TempDir(), "ProgramData", "vmrc", "users", "dev")
	legacy := filepath.Join(appData, "agent.json")
	target := filepath.Join(protected, "agent.json")
	newBin := filepath.Join(protected, "bin")
	oldBin := filepath.Join(appData, "bin")
	if err := os.MkdirAll(appData, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAgentConfig(t, legacy, map[string]any{
		"bin_dir":        oldBin,
		"state_dir":      filepath.Join(appData, "state"),
		"listen_port":    47801,
		"allowed_logins": []string{"alice@example.com"},
		"future_key":     "kept",
	})

	m, err := migrateLegacyUserConfig(legacy, target, newBin)
	if err != nil || m == nil {
		t.Fatalf("migrate: m=%v err=%v", m, err)
	}
	if m.OldBinDir != oldBin || !reflect.DeepEqual(m.AllowedLogins, []string{"alice@example.com"}) {
		t.Fatalf("migration record = %+v", m)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("migrated config missing: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	// bin_dir must point into the protected root: the elevated task must not
	// keep launching the user-writable copy.
	if got["bin_dir"] != newBin {
		t.Errorf("bin_dir = %v, want %q", got["bin_dir"], newBin)
	}
	if got["listen_port"] != float64(47801) || got["future_key"] != "kept" {
		t.Errorf("other keys must be preserved verbatim: %v", got)
	}
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("legacy config should be renamed away, stat err=%v", err)
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Errorf("legacy config should be kept as .migrated: %v", err)
	}
}

func TestMigrateLegacyUserConfigDefaultsOldBinDir(t *testing.T) {
	appData := t.TempDir()
	legacy := filepath.Join(appData, "agent.json")
	writeAgentConfig(t, legacy, map[string]any{"listen_port": 47800})
	m, err := migrateLegacyUserConfig(legacy, filepath.Join(t.TempDir(), "agent.json"), "/new/bin")
	if err != nil || m == nil {
		t.Fatalf("migrate: m=%v err=%v", m, err)
	}
	if want := filepath.Join(appData, "bin"); m.OldBinDir != want {
		t.Errorf("OldBinDir = %q, want the legacy default %q", m.OldBinDir, want)
	}
}

func TestMigrateLegacyUserConfigNoops(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	target := filepath.Join(dir, "target.json")

	// Nothing to migrate.
	if m, err := migrateLegacyUserConfig(legacy, target, "/b"); m != nil || err != nil {
		t.Fatalf("no legacy config: m=%v err=%v", m, err)
	}
	// Target already present: never overwritten, legacy left alone.
	writeAgentConfig(t, legacy, map[string]any{"bin_dir": "/old"})
	writeAgentConfig(t, target, map[string]any{"bin_dir": "/kept"})
	if m, err := migrateLegacyUserConfig(legacy, target, "/b"); m != nil || err != nil {
		t.Fatalf("existing target: m=%v err=%v", m, err)
	}
	if b, _ := os.ReadFile(target); !strings.Contains(string(b), "/kept") {
		t.Fatalf("existing target was overwritten: %s", b)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy must be untouched when no migration happens: %v", err)
	}
}

func TestMigrateLegacyUserConfigRejectsCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(legacy, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := migrateLegacyUserConfig(legacy, filepath.Join(dir, "new.json"), "/b"); err == nil {
		t.Fatal("corrupt legacy config must be reported, not silently dropped")
	}
}

func TestMigrationCleanupLegacyBinary(t *testing.T) {
	oldBin := t.TempDir()
	exe := filepath.Join(oldBin, binaryFileName())
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	(&migration{OldBinDir: oldBin}).cleanupLegacyBinary()
	if _, err := os.Stat(exe); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy binary should be removed, stat err=%v", err)
	}
}

func TestSetupHints(t *testing.T) {
	joined := func(h []string) string { return strings.Join(h, "\n") }
	if h := joined(setupHints("linux", config.ModeUser, config.LayoutAppData, false)); !strings.Contains(h, "loginctl enable-linger") {
		t.Errorf("linux user mode must print the linger hint, got %q", h)
	}
	if h := setupHints("linux", config.ModeSystem, config.LayoutAppData, false); len(h) != 0 {
		t.Errorf("system mode needs no linger hint, got %q", h)
	}
	if h := setupHints("darwin", config.ModeUser, config.LayoutAppData, false); len(h) != 0 {
		t.Errorf("darwin: no hint expected, got %q", h)
	}
	if h := joined(setupHints("windows", config.ModeUser, config.LayoutProtected, false)); !strings.Contains(h, "admin-only") {
		t.Errorf("windows protected layout hint missing, got %q", h)
	}
	if h := joined(setupHints("windows", config.ModeUser, config.LayoutAppData, true)); !strings.Contains(h, "unelevated") {
		t.Errorf("windows --no-elevate hint missing, got %q", h)
	}
}
