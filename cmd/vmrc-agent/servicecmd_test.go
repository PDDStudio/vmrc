package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/urfave/cli/v3"
)

func TestServiceArgs(t *testing.T) {
	args := serviceArgs("/etc/vmrc/agent.json", "vmrc-agent", config.ModeUser)
	want := []string{"run", "--config", "/etc/vmrc/agent.json", "--service-name", "vmrc-agent"}
	if len(args) != len(want) {
		t.Fatalf("serviceArgs len = %d, want %d (%v)", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestServiceArgsSystemModeIncludesSystemFlag(t *testing.T) {
	args := serviceArgs("/etc/vmrc/agent.json", "vmrc-agent", config.ModeSystem)
	found := false
	for _, a := range args {
		if a == "--system" {
			found = true
		}
	}
	if !found {
		// Without it the boot-launched system service resolves user-mode defaults
		// for any field the config file omits.
		t.Fatalf("system install args must include --system: %v", args)
	}
}

func TestServiceArgsUserModeOmitsSystemFlag(t *testing.T) {
	for _, a := range serviceArgs("/x/agent.json", "vmrc-agent", config.ModeUser) {
		if a == "--system" {
			t.Fatalf("user install must not pass --system: run defaults to user mode")
		}
	}
}

func TestServiceArgsCustomName(t *testing.T) {
	args := serviceArgs("/c/agent.json", "foo", config.ModeUser)
	// the baked --service-name must match the registered name so the launched
	// body's kardianos identity matches the installed service.
	found := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--service-name" && args[i+1] == "foo" {
			found = true
		}
	}
	if !found {
		t.Errorf("serviceArgs did not bake --service-name foo: %v", args)
	}
}

func TestManagedExecutablePointsAtBinDir(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()}}
	for _, mode := range []config.Mode{config.ModeUser, config.ModeSystem} {
		cfg, err := resolveConfigForMode(cmd, mode)
		if err != nil {
			t.Fatalf("resolveConfigForMode(%v): %v", mode, err)
		}
		want := filepath.Join(cfg.BinDir, binaryFileName())
		got, err := managedExecutable(cmd, mode)
		if err != nil {
			t.Fatalf("managedExecutable(%v): %v", mode, err)
		}
		if got != want {
			t.Errorf("managedExecutable(%v) = %q, want managed bin path %q", mode, got, want)
		}
	}
}

func TestDoInstallRelocatesBinaryThenRegisters(t *testing.T) {
	srcDir := t.TempDir()
	self := filepath.Join(srcDir, binaryFileName())
	if err := os.WriteFile(self, []byte("AGENT"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{BinDir: filepath.Join(t.TempDir(), "bin")}
	c := &recordingController{}
	if err := doInstall(cfg, self, c); err != nil {
		t.Fatalf("doInstall: %v", err)
	}
	// The service is registered against the managed copy, so install must create
	// it — a direct `install` (no prior `setup`) otherwise points at a missing
	// binary.
	managed := filepath.Join(cfg.BinDir, binaryFileName())
	if b, err := os.ReadFile(managed); err != nil || string(b) != "AGENT" {
		t.Fatalf("managed binary missing/wrong after install: %q err=%v", b, err)
	}
	if len(c.calls) != 1 || c.calls[0] != "install" {
		t.Fatalf("want [install] after relocate, got %v", c.calls)
	}
}

type recordingController struct {
	calls  []string
	status string
}

func (r *recordingController) Control(action string) error {
	r.calls = append(r.calls, action)
	return nil
}
func (r *recordingController) Status() (string, error) { return r.status, nil }

func TestUninstallStopsRunningInstanceFirst(t *testing.T) {
	c := &recordingController{}
	if err := uninstallService(c); err != nil {
		t.Fatalf("uninstallService: %v", err)
	}
	// A bare deregister leaves the live agent running until reboot; uninstall
	// must stop the running instance first.
	if len(c.calls) != 2 || c.calls[0] != "stop" || c.calls[1] != "uninstall" {
		t.Fatalf("want [stop uninstall], got %v", c.calls)
	}
}

func TestBuildControllerUserModeUsesLauncher(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{configFlag(), serviceNameFlag(), systemFlag()}}
	c, err := buildController(cmd, config.ModeUser)
	if err != nil {
		t.Fatalf("buildController user: %v", err)
	}
	if _, ok := c.(launcherController); !ok {
		t.Fatalf("user mode must yield a launcher controller, got %T", c)
	}
}
