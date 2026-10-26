package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/urfave/cli/v3"
)

// runWithConfig runs a bare command carrying the standard flags with --config
// set, invoking fn inside the action so cmd.String("config") is populated.
func runWithConfig(t *testing.T, cfgPath string, fn func(cmd *cli.Command)) {
	t.Helper()
	cmd := &cli.Command{
		Flags:  []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()},
		Action: func(_ context.Context, cmd *cli.Command) error { fn(cmd); return nil },
	}
	if err := cmd.Run(context.Background(), []string{"vmrc-agent", "--config", cfgPath}); err != nil {
		t.Fatalf("cmd.Run: %v", err)
	}
}

func writeAgentConfig(t *testing.T, path string, fields map[string]any) {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateTargetPrefersManagedCopyWhenPresent(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(binDir, binaryFileName())
	if err := os.WriteFile(managed, []byte("MANAGED"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "agent.json")
	writeAgentConfig(t, cfgPath, map[string]any{
		"bin_dir": binDir, "state_dir": filepath.Join(dir, "state"),
		"base_dir": filepath.Join(dir, "files"), "listen_port": 47800,
	})
	// The operator runs `update` from a different location than the service's copy.
	self := filepath.Join(dir, "downloads", "vmrc-agent")
	runWithConfig(t, cfgPath, func(cmd *cli.Command) {
		if got := updateTarget(cmd, config.ModeUser, self); got != managed {
			t.Fatalf("update must replace the managed copy %q, got %q", managed, got)
		}
	})
}

func TestUpdateTargetFallsBackToSelfWithoutManagedCopy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "agent.json")
	writeAgentConfig(t, cfgPath, map[string]any{
		"bin_dir": filepath.Join(dir, "bin"), "state_dir": filepath.Join(dir, "state"),
		"base_dir": filepath.Join(dir, "files"), "listen_port": 47800,
	})
	self := filepath.Join(dir, "vmrc-agent-running")
	runWithConfig(t, cfgPath, func(cmd *cli.Command) {
		if got := updateTarget(cmd, config.ModeUser, self); got != self {
			t.Fatalf("no managed binary present must fall back to self %q, got %q", self, got)
		}
	})
}

func TestUpdateUsesControllerForMode(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()}}
	c, err := buildController(cmd, config.ModeUser)
	if err != nil {
		t.Fatalf("buildController: %v", err)
	}
	if _, ok := c.(launcherController); !ok {
		t.Fatalf("update should drive a launcher controller in user mode, got %T", c)
	}
}
