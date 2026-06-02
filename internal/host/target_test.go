package host

import (
	"slices"
	"testing"
)

func TestParseTargets(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    []string
		wantErr bool
	}{
		{"single", "vm1", []string{"vm1"}, false},
		{"multi", "vm1,vm2", []string{"vm1", "vm2"}, false},
		{"trims spaces", " vm1 , vm2 ", []string{"vm1", "vm2"}, false},
		{"empty spec", "", nil, true},
		{"empty entry", "vm1,,vm2", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTargets(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitTargetPath(t *testing.T) {
	tests := []struct {
		name         string
		spec         string
		wantT, wantP string
		wantErr      bool
	}{
		{"unix", "vm:/var/log/x.log", "vm", "/var/log/x.log", false},
		{"windows first-colon split", `vm:C:\out\a.epk`, "vm", `C:\out\a.epk`, false},
		{"no colon", "vmpath", "", "", true},
		{"empty target", ":/x", "", "", true},
		{"empty path", "vm:", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gt, gp, err := SplitTargetPath(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (gt != tt.wantT || gp != tt.wantP) {
				t.Fatalf("got (%q,%q), want (%q,%q)", gt, gp, tt.wantT, tt.wantP)
			}
		})
	}
}
