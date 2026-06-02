//go:build !windows && !darwin && !linux

package agent

import "fmt"

// ScreenCapturer is a no-op on platforms without a supported capture backend.
type ScreenCapturer struct{}

func (ScreenCapturer) Capture() ([]byte, error) { return CaptureDirect() }

// CaptureDirect is unsupported on platforms without a capture backend.
func CaptureDirect() ([]byte, error) {
	return nil, fmt.Errorf("screenshot unsupported on this platform")
}
