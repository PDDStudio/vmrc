package config

import (
	"errors"
	"os/user"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestResolveUserDirUsesSudoInvoker(t *testing.T) {
	lookup := func(name string) (*user.User, error) {
		if name == "alice" {
			return &user.User{HomeDir: "/home/alice"}, nil
		}
		return nil, errors.New("no such user")
	}
	fallback := func() (string, error) { return "/root", nil }
	// Under sudo, user-mode install must target the invoking user's home, not
	// root's (which os.UserHomeDir would return).
	got, err := resolveUserDir("alice", lookup, fallback)
	if err != nil || got != "/home/alice" {
		t.Fatalf("got %q err=%v, want /home/alice", got, err)
	}
}

func TestResolveUserDirFallsBackWithoutSudo(t *testing.T) {
	fallback := func() (string, error) { return "/current/home", nil }
	got, err := resolveUserDir("", nil, fallback)
	if err != nil || got != "/current/home" {
		t.Fatalf("no SUDO_USER must fall back to os.UserHomeDir, got %q err=%v", got, err)
	}
}

func TestResolveUserDirIgnoresRootInvoker(t *testing.T) {
	fallback := func() (string, error) { return "/root", nil }
	got, err := resolveUserDir("root", nil, fallback)
	if err != nil || got != "/root" {
		t.Fatalf("SUDO_USER=root must fall back, got %q err=%v", got, err)
	}
}

func TestUserPathsWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only path separators")
	}
	got := userPaths("windows", `C:\Users\dev\AppData\Local`)
	want := paths{
		ConfigPath: `C:\Users\dev\AppData\Local\vmrc\agent.json`,
		StateDir:   `C:\Users\dev\AppData\Local\vmrc\state`,
		BaseDir:    `C:\Users\dev\AppData\Local\vmrc\files`,
		BinDir:     `C:\Users\dev\AppData\Local\vmrc\bin`,
	}
	if got != want {
		t.Fatalf("userPaths windows:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestUserPathsUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only path separators")
	}
	got := userPaths("linux", "/home/dev/.local/state")
	want := paths{
		ConfigPath: "/home/dev/.local/state/vmrc/agent.json",
		StateDir:   "/home/dev/.local/state/vmrc/state",
		BaseDir:    "/home/dev/.local/state/vmrc/files",
		BinDir:     "/home/dev/.local/state/vmrc/bin",
	}
	if got != want {
		t.Fatalf("userPaths unix:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestDefaultsForUserModeIsAbsolute(t *testing.T) {
	cfg, err := DefaultsFor(ModeUser)
	if err != nil {
		t.Fatalf("DefaultsFor user: %v", err)
	}
	for _, p := range []string{cfg.StateDir, cfg.BaseDir, cfg.BinDir} {
		if p == "" || !filepath.IsAbs(p) {
			t.Fatalf("user-mode path not absolute: %q", p)
		}
	}
}

func TestChooseUserLayout(t *testing.T) {
	tests := []struct {
		name                          string
		goos                          string
		protectedExists, legacyExists bool
		want                          UserLayout
	}{
		{"unix always app-data", "linux", true, true, LayoutAppData},
		{"darwin always app-data", "darwin", false, false, LayoutAppData},
		{"windows fresh -> protected", "windows", false, false, LayoutProtected},
		{"windows protected install", "windows", true, false, LayoutProtected},
		{"windows protected wins over leftover legacy", "windows", true, true, LayoutProtected},
		{"windows legacy / no-elevate install", "windows", false, true, LayoutAppData},
	}
	for _, tt := range tests {
		if got := chooseUserLayout(tt.goos, tt.protectedExists, tt.legacyExists); got != tt.want {
			t.Errorf("%s: chooseUserLayout = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPreferredUserLayout(t *testing.T) {
	tests := []struct {
		goos      string
		noElevate bool
		want      UserLayout
	}{
		{"windows", false, LayoutProtected},
		{"windows", true, LayoutAppData},
		{"linux", false, LayoutAppData},
		{"darwin", true, LayoutAppData},
	}
	for _, tt := range tests {
		if got := preferredUserLayout(tt.goos, tt.noElevate); got != tt.want {
			t.Errorf("preferredUserLayout(%s, %v) = %v, want %v", tt.goos, tt.noElevate, got, tt.want)
		}
	}
}

func TestAccountDirName(t *testing.T) {
	tests := map[string]string{
		`DESKTOP-1\dev`:    "dev",
		`dev`:              "dev",
		`CORP\first.last`:  "first.last",
		`DOM\we:ird*name?`: "we_ird_name_",
		`..`:               "_",
		``:                 "_",
		`host\a/b`:         "b",
	}
	for in, want := range tests {
		if got := accountDirName(in); got != want {
			t.Errorf("accountDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUserPathsForProtectedLayout(t *testing.T) {
	appBase := filepath.Join("appdata", "Local")
	protected := filepath.Join("ProgramData", "vmrc", "users", "dev")
	got := userPathsFor("windows", appBase, LayoutProtected, protected)
	want := paths{
		// The elevated task launches these: admin-only location.
		ConfigPath: filepath.Join(protected, "agent.json"),
		BinDir:     filepath.Join(protected, "bin"),
		// Per-user data stays in the user's profile.
		StateDir: filepath.Join(appBase, "vmrc", "state"),
		BaseDir:  filepath.Join(appBase, "vmrc", "files"),
	}
	if got != want {
		t.Fatalf("protected layout:\n got=%+v\nwant=%+v", got, want)
	}
	if legacy := userPathsFor("windows", appBase, LayoutAppData, protected); legacy != userPaths("windows", appBase) {
		t.Fatalf("app-data layout must equal userPaths, got %+v", legacy)
	}
}

func TestProtectedUserRootUnderSystemRoot(t *testing.T) {
	sys := filepath.Join("C", "ProgramData", "vmrc")
	if got, want := protectedUserRoot(sys, `PC\dev`), filepath.Join(sys, "users", "dev"); got != want {
		t.Fatalf("protectedUserRoot = %q, want %q", got, want)
	}
}

func TestDefaultsForUserIsAppDataOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("non-windows behavior")
	}
	// Unix behavior is unchanged: the detected layout is the app-data one.
	a, err := DefaultsFor(ModeUser)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DefaultsForLayout(ModeUser, LayoutAppData)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("DefaultsFor(user) = %+v, want app-data layout %+v", a, b)
	}
	p, err := ConfigPathFor(ModeUser)
	if err != nil {
		t.Fatal(err)
	}
	if q, _ := ConfigPathForLayout(ModeUser, LayoutAppData); p != q {
		t.Fatalf("ConfigPathFor(user) = %q, want %q", p, q)
	}
}

func TestIsWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator)+"pd", "vmrc", "users", "dev")
	tests := []struct {
		path string
		want bool
	}{
		{root, true},
		{filepath.Join(root, "bin"), true},
		{filepath.Join(root, "bin", "..", "bin"), true},
		{filepath.Join(root, "..", "other", "bin"), false},
		{filepath.Join(root+"x", "bin"), false},
		{filepath.Join(string(filepath.Separator)+"home", "dev", "bin"), false},
	}
	for _, tt := range tests {
		if got := IsWithin(tt.path, root); got != tt.want {
			t.Errorf("IsWithin(%q, %q) = %v, want %v", tt.path, root, got, tt.want)
		}
	}
}

func TestDefaultsForSystemMatchesLegacy(t *testing.T) {
	cfg, err := DefaultsFor(ModeSystem)
	if err != nil {
		t.Fatalf("DefaultsFor system: %v", err)
	}
	legacy := Defaults()
	if cfg.StateDir != legacy.StateDir || cfg.BaseDir != legacy.BaseDir {
		t.Fatalf("system mode must match legacy defaults: %+v vs %+v", cfg, legacy)
	}
}
