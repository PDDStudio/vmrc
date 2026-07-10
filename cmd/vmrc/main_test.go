package main

import "testing"

// resolveBuildStamp must resolve into the package globals (not throwaway
// locals), so that `update --self` gates and reports on the same version the
// binary shows via --version. A linker-stamped value is authoritative, so
// resolving must preserve it.
func TestResolveBuildStampResolvesIntoGlobals(t *testing.T) {
	ov, oc, od := version, commit, date
	defer func() { version, commit, date = ov, oc, od }()

	version, commit, date = "9.9.9", "cafebabecafe", "2026-01-01T00:00:00Z"
	resolveBuildStamp()

	if version != "9.9.9" {
		t.Errorf("version = %q, want resolved 9.9.9", version)
	}
	if commit != "cafebabecafe" {
		t.Errorf("commit = %q, want cafebabecafe", commit)
	}
	if date != "2026-01-01T00:00:00Z" {
		t.Errorf("date = %q, want 2026-01-01T00:00:00Z", date)
	}
}
