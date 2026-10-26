package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a time.Duration that reads and writes JSON as a Go duration
// string ("168h", "30m", "0"). A bare JSON number is accepted only when it is 0
// (meaning "disabled"); any other number is rejected because its unit would be
// ambiguous (nanoseconds? seconds?).
type Duration time.Duration

// String formats d like time.Duration.
func (d Duration) String() string { return time.Duration(d).String() }

// MarshalJSON writes d as a duration string, e.g. "168h0m0s".
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON parses a duration string (time.ParseDuration syntax) or the
// number 0.
func (d *Duration) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] != '"' {
		var n json.Number
		if err := json.Unmarshal(b, &n); err != nil {
			return fmt.Errorf("duration must be a string like \"168h\": %w", err)
		}
		if f, err := n.Float64(); err != nil || f != 0 {
			return fmt.Errorf("duration %s: use a string like \"168h\" (only a bare 0 is accepted as a number)", n)
		}
		*d = 0
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}
