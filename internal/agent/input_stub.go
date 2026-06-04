//go:build !windows

package agent

import "fmt"

// SystemInjector is unsupported on platforms without an input backend. macOS
// and Linux backends will be added as input_darwin.go / input_linux.go, at
// which point this stub's build tag tightens to exclude them (mirroring the
// capture_unix.go / capture_stub.go split).
type SystemInjector struct{}

func (SystemInjector) Click(int, int, string, bool) error { return errInputUnsupported() }
func (SystemInjector) Type(string) error                  { return errInputUnsupported() }
func (SystemInjector) Key(string) error                   { return errInputUnsupported() }

func errInputUnsupported() error {
	return fmt.Errorf("input injection not supported on this platform")
}
