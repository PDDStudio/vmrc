package selfupdate

import "testing"

func TestAssetName(t *testing.T) {
	tests := []struct {
		bin, tag, goos, goarch string
		want                   string
	}{
		{"vmrc", "v0.1.0", "linux", "amd64", "vmrc_v0.1.0_linux_amd64.tar.gz"},
		{"vmrc", "v0.1.0", "darwin", "arm64", "vmrc_v0.1.0_darwin_arm64.tar.gz"},
		{"vmrc-agent", "v1.2.3", "windows", "amd64", "vmrc-agent_v1.2.3_windows_amd64.zip"},
		{"vmrc-agent", "v1.2.3", "linux", "arm64", "vmrc-agent_v1.2.3_linux_arm64.tar.gz"},
	}
	for _, tt := range tests {
		if got := assetName(tt.bin, tt.tag, tt.goos, tt.goarch); got != tt.want {
			t.Errorf("assetName(%q,%q,%q,%q) = %q, want %q", tt.bin, tt.tag, tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestCompareVersionsAndIsNewer(t *testing.T) {
	tests := []struct {
		a, b string
		want int // sign of compareVersions(a,b)
	}{
		{"v0.1.0", "v0.1.0", 0},
		{"0.1.0", "v0.1.0", 0}, // leading v is irrelevant
		{"v0.2.0", "v0.1.9", 1},
		{"v0.1.0", "v0.2.0", -1},
		{"v1.0.0", "v0.9.9", 1},
		{"v0.1.10", "v0.1.2", 1},    // numeric, not lexical
		{"v1.2.0", "v1.2.0-rc1", 1}, // release beats its prerelease
		{"v1.2.0-rc2", "v1.2.0-rc1", 1},
		{"v1.2.0-rc10", "v1.2.0-rc9", 1},        // numeric prerelease, not lexical ("rc10" < "rc9" lexically)
		{"v1.2.0-rc.10", "v1.2.0-rc.9", 1},      // dot-separated numeric identifier compared numerically
		{"v1.2.0-rc.2", "v1.2.0-rc.2", 0},       // equal prereleases
		{"v1.2.0-alpha", "v1.2.0-alpha.1", -1},  // shorter set has lower precedence when prefix equal
		{"v1.2.0-alpha.1", "v1.2.0-alpha.beta", -1}, // numeric identifier < alphanumeric identifier
		{"v1.2.0-beta", "v1.2.0-alpha", 1},      // alphanumeric compared lexically
		{"v1.0.0", "dev", 1},  // any release beats dev
		{"dev", "v1.0.0", -1}, // dev is older than everything
		{"dev", "dev", 0},
	}
	for _, tt := range tests {
		got := compareVersions(tt.a, tt.b)
		if sign(got) != tt.want {
			t.Errorf("compareVersions(%q,%q) sign = %d, want %d", tt.a, tt.b, sign(got), tt.want)
		}
	}

	if !isNewer("v0.2.0", "v0.1.0") {
		t.Error("isNewer(v0.2.0, v0.1.0) = false, want true")
	}
	if isNewer("v0.1.0", "v0.1.0") {
		t.Error("isNewer(equal) = true, want false")
	}
	if !isNewer("v0.0.1", "dev") {
		t.Error("isNewer(release, dev) = false, want true")
	}
}

func TestGateDevBuild(t *testing.T) {
	tests := []struct {
		version string
		force   bool
		pinned  string
		wantErr bool
	}{
		{"v0.1.0", false, "", false},    // release build: always allowed
		{"dev", false, "", true},        // dev build: refused by default
		{"dev", true, "", false},        // --force opts in
		{"dev", false, "v0.2.0", false}, // explicit --version opts in
	}
	for _, tt := range tests {
		err := GateDevBuild(tt.version, tt.force, tt.pinned)
		if (err != nil) != tt.wantErr {
			t.Errorf("GateDevBuild(%q,%v,%q) err=%v, wantErr=%v", tt.version, tt.force, tt.pinned, err, tt.wantErr)
		}
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}
