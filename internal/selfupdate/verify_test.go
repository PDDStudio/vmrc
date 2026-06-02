package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	// goreleaser emits "<sha256>  <filename>" (two spaces). Include a single-space
	// and a "*" binary-marker variant for tolerance.
	in := strings.Join([]string{
		"aaaa  vmrc_v0.1.0_linux_amd64.tar.gz",
		"bbbb vmrc_v0.1.0_darwin_arm64.tar.gz",
		"CCCC *vmrc-agent_v0.1.0_windows_amd64.zip",
		"", // blank line ignored
	}, "\n")
	got, err := parseChecksums(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parseChecksums: %v", err)
	}
	want := map[string]string{
		"vmrc_v0.1.0_linux_amd64.tar.gz":      "aaaa",
		"vmrc_v0.1.0_darwin_arm64.tar.gz":     "bbbb",
		"vmrc-agent_v0.1.0_windows_amd64.zip": "cccc", // lowercased
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d entries, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("entry %q = %q, want %q", k, got[k], v)
		}
	}

	if _, err := parseChecksums(strings.NewReader("")); err == nil {
		t.Error("parseChecksums(empty) = nil error, want error")
	}
}

func TestVerifySHA256(t *testing.T) {
	data := []byte("the new binary bytes")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])

	if err := verifySHA256(data, good); err != nil {
		t.Errorf("verifySHA256(good) = %v, want nil", err)
	}
	if err := verifySHA256(data, strings.ToUpper(good)); err != nil {
		t.Errorf("verifySHA256(uppercase) = %v, want nil (case-insensitive)", err)
	}
	err := verifySHA256(append(data, 'x'), good)
	if !errors.Is(err, ErrChecksum) {
		t.Errorf("verifySHA256(tampered) = %v, want ErrChecksum", err)
	}
}
