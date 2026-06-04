package buildinfo

import "testing"

func TestResolve(t *testing.T) {
	tests := []struct {
		name                              string
		ldVersion, ldCommit, ldDate       string
		meta                              buildMeta
		wantVersion, wantCommit, wantDate string
	}{
		{
			name:      "ldflags set (goreleaser/make) always win",
			ldVersion: "0.1.1", ldCommit: "abcdef1234567890", ldDate: "2026-06-02T10:00:00Z",
			meta:        buildMeta{mainVersion: "(devel)", revision: "999", available: true},
			wantVersion: "0.1.1", wantCommit: "abcdef1234567890", wantDate: "2026-06-02T10:00:00Z",
		},
		{
			name:      "go install module@version fills all three from build info",
			ldVersion: DevVersion, ldCommit: NoCommit, ldDate: NoDate,
			meta:        buildMeta{mainVersion: "v0.1.1", revision: "abcdef1234567890fedcba", time: "2026-06-02T10:00:00Z", available: true},
			wantVersion: "v0.1.1", wantCommit: "abcdef123456", wantDate: "2026-06-02T10:00:00Z",
		},
		{
			name:      "local go build with VCS info: (devel) keeps dev, commit gets -dirty",
			ldVersion: DevVersion, ldCommit: NoCommit, ldDate: NoDate,
			meta:        buildMeta{mainVersion: "(devel)", revision: "abcdef1234567890", time: "2026-06-04T00:00:00Z", modified: true, available: true},
			wantVersion: DevVersion, wantCommit: "abcdef123456-dirty", wantDate: "2026-06-04T00:00:00Z",
		},
		{
			name:      "no build info available leaves defaults",
			ldVersion: DevVersion, ldCommit: NoCommit, ldDate: NoDate,
			meta:        buildMeta{available: false},
			wantVersion: DevVersion, wantCommit: NoCommit, wantDate: NoDate,
		},
		{
			name:      "empty main version is ignored (not a real version)",
			ldVersion: DevVersion, ldCommit: NoCommit, ldDate: NoDate,
			meta:        buildMeta{mainVersion: "", revision: "deadbeefcafe0000", time: "2026-06-04T00:00:00Z", available: true},
			wantVersion: DevVersion, wantCommit: "deadbeefcafe", wantDate: "2026-06-04T00:00:00Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotV, gotC, gotD := resolve(tt.ldVersion, tt.ldCommit, tt.ldDate, tt.meta)
			if gotV != tt.wantVersion || gotC != tt.wantCommit || gotD != tt.wantDate {
				t.Errorf("resolve() = (%q, %q, %q), want (%q, %q, %q)",
					gotV, gotC, gotD, tt.wantVersion, tt.wantCommit, tt.wantDate)
			}
		})
	}
}
