//go:build darwin || linux

package agent

// ScreenCapturer captures the primary display directly (the process already
// runs in the user's graphical session on macOS/Linux).
type ScreenCapturer struct{}

func (ScreenCapturer) Capture() ([]byte, error) { return CaptureDirect() }
