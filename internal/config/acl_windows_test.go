//go:build windows

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// These tests operate only on t.TempDir() trees. Changing an owner to
// Administrators needs an elevated token, so they skip when not elevated
// (GitHub's windows runners are elevated).
func requireElevatedToken(t *testing.T) {
	t.Helper()
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("needs an elevated (Administrator) token")
	}
}

func sddlOf(t *testing.T, p string) sddlParts {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo(%s): %v", p, err)
	}
	return parseSDDL(sd.String())
}

func setSDDL(t *testing.T, p, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatalf("SetNamedSecurityInfo(%s): %v", p, err)
	}
}

// grantsUsers reports whether any ACE names BUILTIN\Users, Authenticated
// Users, Everyone, or CREATOR OWNER — principals a protected dir must not grant.
func grantsUsers(p sddlParts) bool {
	for _, a := range p.aces {
		f := strings.Split(a, ";")
		switch f[len(f)-1] {
		case "BU", "AU", "WD", "CO", "S-1-5-32-545", "S-1-5-11", "S-1-1-0", "S-1-3-0":
			return true
		}
	}
	return false
}

func TestSecureDirProtectsRootAndResetsChildren(t *testing.T) {
	requireElevatedToken(t)
	root := filepath.Join(t.TempDir(), "vmrc")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(state, "tailscaled.state")
	if err := os.WriteFile(key, []byte("KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An explicit ACE granting Users full control must be removed.
	setSDDL(t, key, "D:P(A;;FA;;;BU)(A;;FA;;;BA)")

	sid, err := CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if err := SecureDir(root, sid, nil); err != nil {
		t.Fatalf("SecureDir: %v", err)
	}

	r := sddlOf(t, root)
	if !ownerIsTrusted(r.owner) || !hasDACLFlag(r.daclFlags, "P") {
		t.Fatalf("root: want trusted owner + protected DACL, got %+v", r)
	}
	if len(r.aces) != 3 || grantsUsers(r) {
		t.Fatalf("root: want exactly SYSTEM, Administrators, reader; got %+v", r.aces)
	}
	for _, p := range []string{state, key} {
		c := sddlOf(t, p)
		if needsChildReset(sddlString(t, p)) || grantsUsers(c) {
			t.Fatalf("%s: want inherited-only ACEs without Users, got %+v", p, c)
		}
	}
	// Idempotent: a second run (the startup repair) succeeds and changes nothing.
	if err := SecureDir(root, sid, nil); err != nil {
		t.Fatalf("second SecureDir: %v", err)
	}
}

func sddlString(t *testing.T, p string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

func TestSecureDirNoDescend(t *testing.T) {
	requireElevatedToken(t)
	root := filepath.Join(t.TempDir(), "vmrc")
	files := filepath.Join(root, "files")
	if err := os.MkdirAll(files, 0o755); err != nil {
		t.Fatal(err)
	}
	upload := filepath.Join(files, "upload.bin")
	if err := os.WriteFile(upload, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	setSDDL(t, upload, "D:P(A;;FA;;;BU)(A;;FA;;;BA)")
	if err := SecureDir(root, "", []string{"files"}); err != nil {
		t.Fatalf("SecureDir: %v", err)
	}
	if needsChildReset(sddlString(t, files)) {
		t.Fatalf("a noDescend dir itself must still be reset")
	}
	if !grantsUsers(sddlOf(t, upload)) {
		t.Fatalf("entries inside a noDescend dir must be left alone")
	}
}

func TestSecureDirRefusesReparsePoints(t *testing.T) {
	requireElevatedToken(t)
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	before := sddlString(t, outside)

	// A junction inside the protected dir must not get its target re-ACLed.
	root := filepath.Join(base, "vmrc")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if out, err := exec.Command("cmd.exe", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("cannot create junction: %v %s", err, out)
	}
	if err := SecureDir(root, "", nil); err == nil || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("want a reparse-point error, got %v", err)
	}
	if after := sddlString(t, outside); after != before {
		t.Fatalf("junction target was modified:\n before %s\n after  %s", before, after)
	}

	// The protected dir itself being a junction is refused outright.
	if err := SecureDir(link, "", nil); err == nil || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("want refusal for a junction root, got %v", err)
	}
}
