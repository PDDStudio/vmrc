package agent

import "testing"

func TestCmdShellLineKeepsQuotesVerbatim(t *testing.T) {
	tests := []struct{ in, want string }{
		{`echo hi`, `cmd.exe /s /c "echo hi"`},
		{`echo "a b"`, `cmd.exe /s /c "echo "a b""`},
		{`"C:\Program Files\x\y.exe" --flag "v w"`, `cmd.exe /s /c ""C:\Program Files\x\y.exe" --flag "v w""`},
	}
	for _, tt := range tests {
		if got := cmdShellLine(tt.in); got != tt.want {
			t.Errorf("cmdShellLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
