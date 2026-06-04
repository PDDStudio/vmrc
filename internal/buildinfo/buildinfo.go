// Package buildinfo resolves the version/commit/date a binary reports from
// `--version`, preferring linker-stamped values and falling back to the Go
// module build info embedded by the toolchain.
package buildinfo

import "runtime/debug"

// Sentinel defaults the cmd/* main packages initialise their stamp vars to.
// When the linker overrides them (GoReleaser / `make build`), the stamped value
// is authoritative; while still at these sentinels, Resolve consults build info.
const (
	DevVersion = "dev"
	NoCommit   = "none"
	NoDate     = "unknown"
)

// Resolve returns the version, commit, and date to report from `--version`,
// preferring the linker-stamped arguments and falling back to the Go module
// build info embedded by the toolchain. Pass the cmd/* package's stamp vars.
func Resolve(ldVersion, ldCommit, ldDate string) (version, commit, date string) {
	return resolve(ldVersion, ldCommit, ldDate, readBuildMeta())
}

// readBuildMeta extracts the fields resolve() needs from the running binary's
// build info, if any.
func readBuildMeta() buildMeta {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return buildMeta{available: false}
	}
	m := buildMeta{available: true, mainVersion: bi.Main.Version}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			m.revision = s.Value
		case "vcs.time":
			m.time = s.Value
		case "vcs.modified":
			m.modified = s.Value == "true"
		}
	}
	return m
}

// buildMeta is the slice of runtime/debug.BuildInfo this package cares about,
// lifted out so resolve() stays pure and testable.
type buildMeta struct {
	mainVersion string // BuildInfo.Main.Version
	revision    string // vcs.revision setting
	time        string // vcs.time setting
	modified    bool   // vcs.modified setting
	available   bool   // ReadBuildInfo succeeded
}

// shortCommitLen is how many leading hex chars of a VCS revision to display;
// long enough to stay unique in practice, short enough to read.
const shortCommitLen = 12

// resolve merges linker stamps with build-info fallback. Pure for testing.
//
// Linker-stamped values (ldVersion != DevVersion) always win: that is a release
// or `make` build and its stamps are authoritative. Otherwise each field still
// at its sentinel is filled from the module build info, so `go install
// module@version` and plain `go build` report something meaningful.
func resolve(ldVersion, ldCommit, ldDate string, m buildMeta) (version, commit, date string) {
	version, commit, date = ldVersion, ldCommit, ldDate
	if ldVersion != DevVersion || !m.available {
		return version, commit, date
	}
	// Module version from `go install module@vX.Y.Z`. "(devel)" and "" are not
	// real release versions, so leave the dev sentinel in place for those.
	if version == DevVersion && m.mainVersion != "" && m.mainVersion != "(devel)" {
		version = m.mainVersion
	}
	if commit == NoCommit && m.revision != "" {
		commit = m.revision
		if len(commit) > shortCommitLen {
			commit = commit[:shortCommitLen]
		}
		if m.modified {
			commit += "-dirty"
		}
	}
	if date == NoDate && m.time != "" {
		date = m.time
	}
	return version, commit, date
}
