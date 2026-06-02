package selfupdate

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// parseChecksums reads goreleaser / coreutils-style "<hex>  <filename>" lines
// into a filename->hex map. It tolerates single- or double-space separators and
// a leading "*" binary marker on the filename. goreleaser filenames contain no
// spaces, so the last field is the name.
func parseChecksums(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		out[name] = strings.ToLower(fields[0])
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no checksum entries found")
	}
	return out, nil
}

// verifySHA256 checks data's SHA-256 against the hex want, returning ErrChecksum
// (wrapped, with both digests) on mismatch.
func verifySHA256(data []byte, want string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, strings.TrimSpace(want)) {
		return fmt.Errorf("%w: have %s, want %s", ErrChecksum, got, want)
	}
	return nil
}
