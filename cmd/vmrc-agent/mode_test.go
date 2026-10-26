package main

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/pddstudio/vmrc/internal/config"
	"github.com/urfave/cli/v3"
)

func TestPrivilegeCheck(t *testing.T) {
	errNotElevated := errors.New("not elevated")
	notElevated := func() error { return errNotElevated }
	elevated := func() error { return nil }
	tests := []struct {
		name         string
		goos         string
		mode         config.Mode
		action       string
		elevatedTask bool
		euid         int
		sudoUser     string
		probe        func() error
		wantErr      string // "" = allowed; "elevate" = the probe's error; else a substring
	}{
		{"system setup needs elevation", "linux", config.ModeSystem, "setup", false, 1000, "", notElevated, "elevate"},
		{"system setup as root", "linux", config.ModeSystem, "setup", false, 0, "alice", elevated, ""},
		{"system status free", "windows", config.ModeSystem, "status", false, 1000, "", notElevated, ""},
		{"system update self-gates", "windows", config.ModeSystem, "update", false, 1000, "", notElevated, ""},

		{"win elevated setup", "windows", config.ModeUser, "setup", true, -1, "", notElevated, "elevate"},
		{"win no-elevate setup", "windows", config.ModeUser, "setup", false, -1, "", notElevated, ""},
		{"win elevated update", "windows", config.ModeUser, "update", true, -1, "", notElevated, "elevate"},
		{"win legacy update", "windows", config.ModeUser, "update", false, -1, "", notElevated, ""},
		{"win uninstall elevated", "windows", config.ModeUser, "uninstall", true, -1, "", notElevated, "elevate"},
		{"win start free", "windows", config.ModeUser, "start", true, -1, "", notElevated, ""},

		{"linux user setup as user", "linux", config.ModeUser, "setup", true, 1000, "", notElevated, ""},
		{"linux user setup under sudo", "linux", config.ModeUser, "setup", true, 0, "alice", elevated, "without sudo as alice"},
		{"darwin user install as root", "darwin", config.ModeUser, "install", true, 0, "", elevated, "--system"},
		{"linux user stop under sudo", "linux", config.ModeUser, "stop", true, 0, "alice", elevated, "must not run"},
		{"linux user status as root ok", "linux", config.ModeUser, "status", true, 0, "alice", elevated, ""},
	}
	for _, tt := range tests {
		err := privilegeCheck(tt.goos, tt.mode, tt.action, tt.elevatedTask, tt.euid, tt.sudoUser, tt.probe)
		switch {
		case tt.wantErr == "" && err != nil:
			t.Errorf("%s: want allowed, got %v", tt.name, err)
		case tt.wantErr == "elevate" && !errors.Is(err, errNotElevated):
			t.Errorf("%s: want the elevation error, got %v", tt.name, err)
		case tt.wantErr != "" && tt.wantErr != "elevate" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
			t.Errorf("%s: want error containing %q, got %v", tt.name, tt.wantErr, err)
		}
	}
}

func TestUserLayoutForRegisteringCommands(t *testing.T) {
	run := func(flags []cli.Flag, args []string) config.UserLayout {
		t.Helper()
		var got config.UserLayout
		cmd := &cli.Command{Flags: flags, Action: func(_ context.Context, cmd *cli.Command) error {
			got = userLayout(cmd, config.ModeUser)
			return nil
		}}
		if err := cmd.Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		return got
	}
	setupFlags := []cli.Flag{configFlag(), systemFlag(), noElevateFlag()}
	if got := run(setupFlags, []string{"x", "--no-elevate"}); got != config.LayoutAppData {
		t.Errorf("--no-elevate must select the app-data layout, got %v", got)
	}
	wantDefault := config.LayoutAppData
	if runtime.GOOS == "windows" {
		wantDefault = config.LayoutProtected
	}
	if got := run(setupFlags, []string{"x"}); got != wantDefault {
		t.Errorf("setup default layout = %v, want %v", got, wantDefault)
	}
}

func TestValidateNoElevateRejectsSystemMode(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{systemFlag(), noElevateFlag()}, Action: func(_ context.Context, cmd *cli.Command) error {
		return validateNoElevate(cmd, resolveMode(cmd))
	}}
	if err := cmd.Run(context.Background(), []string{"x", "--system", "--no-elevate"}); err == nil {
		t.Fatal("--system --no-elevate must be rejected")
	}
}

func TestResolveModeDefaultsToUser(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{systemFlag()}}
	if got := resolveMode(cmd); got != config.ModeUser {
		t.Fatalf("default mode = %q, want user", got)
	}
}
