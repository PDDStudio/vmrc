package selfupdate

import (
	"regexp"
	"strconv"
	"strings"
)

// archiveExt is the goreleaser archive format extension for a GOOS.
func archiveExt(goos string) string {
	if goos == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}

// assetName builds the goreleaser archive filename for a binary/os/arch. It
// mirrors .goreleaser.yaml's name_template
// "{{ .Binary }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}" plus the format ext.
func assetName(bin, tag, goos, goarch string) string {
	return bin + "_" + tag + "_" + goos + "_" + goarch + archiveExt(goos)
}

// normalizeVersion strips a leading "v" and surrounding whitespace.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// splitPre separates a version's numeric core from its prerelease suffix.
func splitPre(v string) (core, pre string) {
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

// isReleaseVersion reports whether v's core is dotted decimal (so "dev",
// "", and other non-numeric strings are not release versions).
func isReleaseVersion(v string) bool {
	core, _ := splitPre(v)
	if core == "" {
		return false
	}
	for _, seg := range strings.Split(core, ".") {
		if seg == "" {
			return false
		}
		if _, err := strconv.Atoi(seg); err != nil {
			return false
		}
	}
	return true
}

// compareVersions returns -1, 0, or 1 comparing a vs b with basic SemVer
// precedence: numeric core compared segment-by-segment, and a prerelease
// ("1.2.0-rc1") sorts below its release ("1.2.0"). Non-release versions (e.g.
// "dev") sort below any release version.
func compareVersions(a, b string) int {
	na, nb := normalizeVersion(a), normalizeVersion(b)
	ra, rb := isReleaseVersion(na), isReleaseVersion(nb)
	switch {
	case !ra && !rb:
		return strings.Compare(na, nb)
	case !ra:
		return -1
	case !rb:
		return 1
	}
	coreA, preA := splitPre(na)
	coreB, preB := splitPre(nb)
	if c := compareCore(coreA, coreB); c != 0 {
		return c
	}
	switch {
	case preA == "" && preB == "":
		return 0
	case preA == "": // release > prerelease
		return 1
	case preB == "":
		return -1
	default:
		return comparePre(preA, preB)
	}
}

// comparePre compares two prerelease strings (the part after '-') by SemVer §11
// precedence: dot-separated identifiers compared left to right, a purely numeric
// identifier ranking below an alphanumeric one, and — when the common prefix is
// equal — the larger set of identifiers winning. Within an alphanumeric
// identifier it uses natural (digit-aware) ordering so the project's glued
// "rc9"/"rc10" tag style sorts numerically rather than lexically.
func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := comparePreIdent(as[i], bs[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// comparePreIdent compares one prerelease identifier.
func comparePreIdent(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	aNumeric, bNumeric := aErr == nil, bErr == nil
	switch {
	case aNumeric && bNumeric:
		return cmpInt(an, bn)
	case aNumeric: // numeric identifier ranks below alphanumeric
		return -1
	case bNumeric:
		return 1
	default:
		return naturalCompare(a, b)
	}
}

// naturalCompare orders two strings treating embedded digit runs as numbers, so
// "rc9" < "rc10". Non-digit runs compare byte-wise.
func naturalCompare(a, b string) int {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		if isDigit(a[ia]) && isDigit(b[ib]) {
			ja, jb := ia, ib
			for ja < len(a) && isDigit(a[ja]) {
				ja++
			}
			for jb < len(b) && isDigit(b[jb]) {
				jb++
			}
			na, _ := strconv.Atoi(a[ia:ja])
			nb, _ := strconv.Atoi(b[ib:jb])
			if c := cmpInt(na, nb); c != 0 {
				return c
			}
			ia, ib = ja, jb
			continue
		}
		if a[ia] != b[ib] {
			if a[ia] < b[ib] {
				return -1
			}
			return 1
		}
		ia++
		ib++
	}
	switch {
	case ia < len(a):
		return 1
	case ib < len(b):
		return -1
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compareCore compares two dotted-decimal version cores.
func compareCore(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(as) {
			ai, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bi, _ = strconv.Atoi(bs[i])
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return 0
}

// isNewer reports whether remote is a strictly newer version than current.
func isNewer(remote, current string) bool {
	return compareVersions(remote, current) > 0
}

// localBuild matches version strings stamped on builds made from source rather
// than a release: `git describe` past a tag (-3-gabc1234), a dirty tree
// (-dirty / +dirty), and Go pseudo-versions from `go install` (a 14-digit UTC
// timestamp followed by a 12-hex commit).
var localBuild = regexp.MustCompile(`-\d+-g[0-9a-f]{4,}|[-+]dirty$|\d{14}-[0-9a-f]{12}`)

// isDevBuild reports whether v is not a published release: non-numeric
// ("dev", "") or a locally built version.
func isDevBuild(v string) bool {
	v = normalizeVersion(v)
	return !isReleaseVersion(v) || localBuild.MatchString(v)
}

// IsDowngrade reports whether installing target would move a release build
// back to an older release. A dev build has no release to protect.
func IsDowngrade(target, current string) bool {
	if isDevBuild(current) {
		return false
	}
	return compareVersions(target, current) < 0
}
