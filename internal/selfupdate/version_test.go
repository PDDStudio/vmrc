package selfupdate

import "testing"

func TestGateDevBuildTreatsLocalBuildsAsDev(t *testing.T) {
	cases := []struct {
		version string
		dev     bool
	}{
		{"v0.2.1", false},
		{"0.3.0-rc1", false},
		{"v0.1.0-3-gabc1234", true},       // git describe, commits past a tag
		{"v0.1.0-3-gabc1234-dirty", true}, // ...with local changes
		{"v0.1.0-dirty", true},
		{"v0.2.2-0.20261026102742-ba5dc846a3d9", true},       // go install pseudo-version
		{"v0.2.2-0.20261026102742-ba5dc846a3d9+dirty", true}, // ...from a dirty tree
		{"v0.0.0-20261026102742-ba5dc846a3d9", true},
		{"dev", true},
	}
	for _, tc := range cases {
		err := GateDevBuild(tc.version, false, "")
		if gotDev := err != nil; gotDev != tc.dev {
			t.Errorf("GateDevBuild(%q) dev=%v, want %v (err=%v)", tc.version, gotDev, tc.dev, err)
		}
	}
}

func TestIsDowngrade(t *testing.T) {
	cases := []struct {
		target, current string
		want            bool
	}{
		{"v0.2.0", "v0.3.0", true},
		{"v0.3.0-rc1", "v0.3.0", true},
		{"v0.3.0", "v0.3.0", false}, // reinstall
		{"v0.3.1", "v0.3.0", false},
		{"v0.2.0", "dev", false}, // nothing to protect on a dev build
		{"v0.2.0", "v0.3.0-2-gabc1234", false},
	}
	for _, tc := range cases {
		if got := IsDowngrade(tc.target, tc.current); got != tc.want {
			t.Errorf("IsDowngrade(%q, %q) = %v, want %v", tc.target, tc.current, got, tc.want)
		}
	}
}
