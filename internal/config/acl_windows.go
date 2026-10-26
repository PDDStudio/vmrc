//go:build windows

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SecureDir creates dir if needed and locks it down (see acl.go):
//
//  1. dir and its ancestors must not be reparse points (junctions/symlinks),
//     so the DACL cannot be redirected onto another directory;
//  2. dir gets owner Administrators and a protected DACL — SYSTEM +
//     Administrators full control, readerSID (if non-empty) read+execute, no
//     inheritance from the parent. It is only rewritten when it differs, so the
//     startup repair does not re-propagate through a large tree every time;
//  3. every entry below dir is reset to owner Administrators with inherited
//     ACEs only (explicit ACEs and foreign owners removed). Reparse points and
//     multiply-linked files are never touched (that would change their target)
//     and are reported as errors. Directories directly under dir whose name is
//     in noDescend are reset themselves but not descended into (another
//     protected dir, or a large data tree that plain inheritance already covers).
//
// It needs an elevated token (or SYSTEM). SeTakeOwnership/SeRestore/SeBackup
// are enabled for the duration of the call so foreign-owned entries can be
// reclaimed, then restored to their previous state.
func SecureDir(dir, readerSID string, noDescend []string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := refuseReparseAncestry(dir); err != nil {
		return err
	}
	restore := enablePrivileges("SeTakeOwnershipPrivilege", "SeRestorePrivilege", "SeBackupPrivilege")
	defer restore()

	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("administrators SID: %w", err)
	}
	want, err := windows.SecurityDescriptorFromString("O:BA" + ProtectedDACLSDDL(readerSID))
	if err != nil {
		return fmt.Errorf("building security descriptor: %w", err)
	}
	dacl, _, err := want.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("building DACL: %v", err)
	}
	cur, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || needsRootRepair(cur.String(), want.String()) {
		if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
			windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
			admins, nil, dacl, nil); err != nil {
			return fmt.Errorf("securing %s: %w", dir, err)
		}
	}
	return resetChildren(dir, admins, noDescend)
}

// CurrentUserSID returns the SID string of the account this process runs as.
func CurrentUserSID() (string, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("reading process token user: %w", err)
	}
	return tu.User.Sid.String(), nil
}

// resetChildren walks root and resets each entry to owner + inherited-only
// DACL where needed (see SecureDir).
func resetChildren(root string, owner *windows.SID, noDescend []string) error {
	emptySD, err := windows.SecurityDescriptorFromString("D:")
	if err != nil {
		return fmt.Errorf("building empty DACL: %w", err)
	}
	empty, _, err := emptySD.DACL()
	if err != nil || empty == nil {
		// A nil DACL would mean "everyone full control" — never pass one on.
		return fmt.Errorf("building empty DACL: %v", err)
	}
	skip := make(map[string]bool, len(noDescend))
	for _, n := range noDescend {
		skip[strings.ToLower(n)] = true
	}

	var errs []error
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			if d != nil && d.IsDir() && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if p == root {
			return nil
		}
		attrs, links, ierr := fileAttrs(p)
		switch {
		case ierr != nil:
			errs = append(errs, ierr)
			return skipIfDir(d)
		case attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0:
			errs = append(errs, fmt.Errorf("%s is a reparse point (junction/symlink) inside a protected dir; left untouched — remove it", p))
			return skipIfDir(d)
		case !d.IsDir() && links > 1:
			errs = append(errs, fmt.Errorf("%s has %d hard links; left untouched — remove it", p, links))
			return nil
		}
		cur, gerr := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if gerr != nil || needsChildReset(cur.String()) {
			if serr := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
				windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION,
				owner, nil, empty, nil); serr != nil {
				errs = append(errs, fmt.Errorf("resetting %s to inherited permissions: %w", p, serr))
			}
		}
		if d.IsDir() && filepath.Dir(p) == root && skip[strings.ToLower(d.Name())] {
			return fs.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	return errors.Join(errs...)
}

func skipIfDir(d fs.DirEntry) error {
	if d != nil && d.IsDir() {
		return fs.SkipDir
	}
	return nil
}

// refuseReparseAncestry fails if dir or any ancestor is a reparse point: a
// junction planted at e.g. C:\ProgramData\vmrc before the first install would
// otherwise redirect the DACL (and the files written next) to its target.
func refuseReparseAncestry(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for p := abs; ; {
		attrs, _, err := fileAttrs(p)
		if err != nil {
			return err
		}
		if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("refusing to secure %s: %s is a reparse point (junction/symlink); remove it and re-run", dir, p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

// fileAttrs returns p's attributes and hard-link count without following a
// reparse point at p itself.
func fileAttrs(p string) (attrs, links uint32, err error) {
	p16, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return 0, 0, err
	}
	h, err := windows.CreateFile(p16, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("inspecting %s: %w", p, err)
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return 0, 0, fmt.Errorf("inspecting %s: %w", p, err)
	}
	return info.FileAttributes, info.NumberOfLinks, nil
}

// enablePrivileges enables the named privileges on the process token (those
// the token holds; others are silently skipped) and returns a func restoring
// their previous state.
func enablePrivileges(names ...string) (restore func()) {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		return func() {}
	}
	var prevs []windows.Tokenprivileges
	for _, name := range names {
		n16, err := windows.UTF16PtrFromString(name)
		if err != nil {
			continue
		}
		var luid windows.LUID
		if err := windows.LookupPrivilegeValue(nil, n16, &luid); err != nil {
			continue
		}
		tp := windows.Tokenprivileges{PrivilegeCount: 1}
		tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
		var prev windows.Tokenprivileges
		var retLen uint32
		if err := windows.AdjustTokenPrivileges(tok, false, &tp, uint32(unsafe.Sizeof(prev)), &prev, &retLen); err == nil && prev.PrivilegeCount > 0 {
			prevs = append(prevs, prev)
		}
	}
	return func() {
		for i := len(prevs) - 1; i >= 0; i-- {
			_ = windows.AdjustTokenPrivileges(tok, false, &prevs[i], 0, nil, nil)
		}
		_ = tok.Close()
	}
}
