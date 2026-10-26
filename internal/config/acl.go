package config

import "strings"

// Windows install roots (C:\ProgramData\vmrc, and the per-user admin-only dir an
// elevated user-mode install launches from) must not inherit C:\ProgramData's
// DACL, which lets every local user read subfolders and create files in them:
// the root holds the tsnet node private key, the request log (every command
// line), and the binary a SYSTEM service or an elevated logon task runs.
//
// SecureDir (acl_windows.go) applies an explicit, protected (non-inherited)
// DACL — SYSTEM and Administrators full control, optionally one account
// read+execute — makes Administrators the owner, and resets everything below
// to plain inheritance. The SDDL construction and the checks that decide
// whether a repair is needed are pure string logic here, so they are unit
// tested on every OS. On non-Windows SecureDir is a no-op (Unix uses mode bits).

const (
	// sddlSystemFull / sddlAdminsFull: full control, inherited by subfolders and
	// files (OICI), for LocalSystem and BUILTIN\Administrators.
	sddlSystemFull = "(A;OICI;FA;;;SY)"
	sddlAdminsFull = "(A;OICI;FA;;;BA)"
	// fileGenericReadExecute is FILE_GENERIC_READ | FILE_GENERIC_EXECUTE (the
	// "Read & execute" permission), granted to the reader account.
	fileGenericReadExecute = "0x1200a9"
)

// ProtectedDACLSDDL returns the SDDL DACL for a protected install dir: SYSTEM +
// Administrators full control, plus read+execute for readerSID when it is
// non-empty, all inherited by children; "P" blocks inheritance from the parent.
func ProtectedDACLSDDL(readerSID string) string {
	s := "D:P" + sddlSystemFull + sddlAdminsFull
	if readerSID != "" {
		s += "(A;OICI;" + fileGenericReadExecute + ";;;" + readerSID + ")"
	}
	return s
}

// sddlParts is the subset of an SDDL security descriptor string we inspect.
type sddlParts struct {
	owner     string   // "O:" value, e.g. "BA" or "S-1-5-21-…"
	daclFlags string   // DACL control flags, e.g. "PAI"
	aces      []string // DACL ACE strings without parentheses
}

// parseSDDL splits an SDDL string ("O:…G:…D:flags(ace)(ace)S:…") into owner,
// DACL flags and DACL ACEs. Section tags are only recognised outside ACE
// parentheses, so ACE contents never confuse the split.
func parseSDDL(s string) sddlParts {
	var p sddlParts
	for len(s) >= 2 && s[1] == ':' {
		tag := s[0]
		s = s[2:]
		end := len(s)
		depth := 0
		for i := 0; i+1 < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 && i > 0 && s[i+1] == ':' && strings.IndexByte("OGDS", s[i]) >= 0 {
				end = i
				break
			}
		}
		val := s[:end]
		s = s[end:]
		switch tag {
		case 'O':
			p.owner = val
		case 'D':
			flags, rest, _ := strings.Cut(val, "(")
			p.daclFlags = flags
			if rest != "" {
				for _, a := range strings.Split("("+rest, ")") {
					if a = strings.TrimPrefix(a, "("); a != "" {
						p.aces = append(p.aces, a)
					}
				}
			}
		}
	}
	return p
}

// ownerIsTrusted reports whether an SDDL owner is Administrators or SYSTEM
// (alias or numeric form). Any other owner keeps implicit WRITE_DAC and could
// re-open a file or dir we protected.
func ownerIsTrusted(owner string) bool {
	switch owner {
	case "BA", "SY", "S-1-5-32-544", "S-1-5-18":
		return true
	}
	return false
}

// hasDACLFlag reports whether the DACL control flags contain flag ("P" =
// protected, "AI" = auto-inherited, …). Flags are matched as whole tokens.
func hasDACLFlag(flags, flag string) bool {
	for _, tok := range []string{"NO_ACCESS_CONTROL", "AR", "AI", "P"} {
		if strings.Contains(flags, tok) {
			if tok == flag {
				return true
			}
			flags = strings.ReplaceAll(flags, tok, "")
		}
	}
	return false
}

// aceInherited reports whether an ACE string carries the ID (inherited) flag.
// ACE flags are concatenated two-letter codes in the second field.
func aceInherited(ace string) bool {
	fields := strings.Split(ace, ";")
	if len(fields) < 2 {
		return false
	}
	for f := fields[1]; len(f) >= 2; f = f[2:] {
		if f[:2] == "ID" {
			return true
		}
	}
	return false
}

// needsRootRepair reports whether a protected root whose current SDDL is
// current must be re-secured to match desired (both as rendered by Windows):
// the owner is not Administrators/SYSTEM, the DACL is not protected, or its
// ACEs differ from the desired set.
func needsRootRepair(current, desired string) bool {
	c, d := parseSDDL(current), parseSDDL(desired)
	if !ownerIsTrusted(c.owner) || !hasDACLFlag(c.daclFlags, "P") {
		return true
	}
	if len(c.aces) != len(d.aces) {
		return true
	}
	want := make(map[string]int, len(d.aces))
	for _, a := range d.aces {
		want[strings.ToUpper(a)]++
	}
	for _, a := range c.aces {
		k := strings.ToUpper(a)
		if want[k] == 0 {
			return true
		}
		want[k]--
	}
	return false
}

// needsChildReset reports whether an entry below a protected root must be
// reset to plain inheritance: it has an untrusted owner, a protected DACL, no
// ACEs at all, or any explicit (non-inherited) ACE.
func needsChildReset(current string) bool {
	c := parseSDDL(current)
	if !ownerIsTrusted(c.owner) || hasDACLFlag(c.daclFlags, "P") || len(c.aces) == 0 {
		return true
	}
	for _, a := range c.aces {
		if !aceInherited(a) {
			return true
		}
	}
	return false
}
