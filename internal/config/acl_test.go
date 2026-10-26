package config

import (
	"reflect"
	"testing"
)

func TestProtectedDACLSDDL(t *testing.T) {
	if got, want := ProtectedDACLSDDL(""), "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"; got != want {
		t.Errorf("ProtectedDACLSDDL(\"\") = %q, want %q", got, want)
	}
	const sid = "S-1-5-21-1-2-3-1001"
	want := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;" + sid + ")"
	if got := ProtectedDACLSDDL(sid); got != want {
		t.Errorf("ProtectedDACLSDDL(sid) = %q, want %q", got, want)
	}
}

func TestParseSDDL(t *testing.T) {
	tests := []struct {
		in   string
		want sddlParts
	}{
		{
			"O:BAD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)",
			sddlParts{owner: "BA", daclFlags: "PAI", aces: []string{"A;OICI;FA;;;SY", "A;OICI;FA;;;BA"}},
		},
		{
			"O:S-1-5-21-1-2-3-1001G:S-1-5-21-1-2-3-513D:AI(A;ID;FA;;;SY)(A;OICIID;0x1200a9;;;BU)S:AI",
			sddlParts{owner: "S-1-5-21-1-2-3-1001", daclFlags: "AI", aces: []string{"A;ID;FA;;;SY", "A;OICIID;0x1200a9;;;BU"}},
		},
		{"O:DAD:", sddlParts{owner: "DA"}},
		{"O:SYD:NO_ACCESS_CONTROL", sddlParts{owner: "SY", daclFlags: "NO_ACCESS_CONTROL"}},
	}
	for _, tt := range tests {
		if got := parseSDDL(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseSDDL(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestNeedsRootRepair(t *testing.T) {
	desired := "O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	tests := []struct {
		name, current string
		want          bool
	}{
		{"already protected", "O:BAD:PAI(A;OICI;FA;;;BA)(A;OICI;FA;;;SY)", false},
		{"system owner ok", "O:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", false},
		{"inherited from ProgramData", "O:BAD:AI(A;OICIID;FA;;;SY)(A;OICIID;FA;;;BA)(A;OICIID;0x1200a9;;;BU)(A;CIID;DCLCRPCR;;;BU)", true},
		{"user owned", "O:S-1-5-21-1-2-3-1001D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", true},
		{"extra users ACE", "O:BAD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;BU)", true},
		{"missing admins", "O:BAD:PAI(A;OICI;FA;;;SY)", true},
		{"unreadable", "", true},
	}
	for _, tt := range tests {
		if got := needsRootRepair(tt.current, desired); got != tt.want {
			t.Errorf("%s: needsRootRepair = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestNeedsChildReset(t *testing.T) {
	tests := []struct {
		name, current string
		want          bool
	}{
		{"plain inheritance", "O:BAD:AI(A;OICIID;FA;;;SY)(A;OICIID;FA;;;BA)", false},
		{"system owned file", "O:SYD:AI(A;ID;FA;;;SY)(A;ID;FA;;;BA)", false},
		{"user owned", "O:S-1-5-21-1-2-3-1001D:AI(A;ID;FA;;;SY)(A;ID;FA;;;BA)", true},
		{"explicit ACE", "O:BAD:AI(A;;FA;;;BU)(A;ID;FA;;;SY)(A;ID;FA;;;BA)", true},
		{"protected", "O:BAD:PAI(A;;FA;;;SY)", true},
		{"no ACEs", "O:BAD:AI", true},
		{"null DACL", "O:BAD:NO_ACCESS_CONTROL", true},
	}
	for _, tt := range tests {
		if got := needsChildReset(tt.current); got != tt.want {
			t.Errorf("%s: needsChildReset = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHasDACLFlag(t *testing.T) {
	tests := []struct {
		flags, flag string
		want        bool
	}{
		{"PAI", "P", true},
		{"PAI", "AI", true},
		{"AI", "P", false},
		{"AR", "P", false},
		{"PARAI", "P", true},
		{"", "P", false},
	}
	for _, tt := range tests {
		if got := hasDACLFlag(tt.flags, tt.flag); got != tt.want {
			t.Errorf("hasDACLFlag(%q, %q) = %v, want %v", tt.flags, tt.flag, got, tt.want)
		}
	}
}
