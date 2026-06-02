//go:build windows || darwin || linux

package agent

import (
	"bytes"
	"fmt"
	"image/png"

	"github.com/kbinani/screenshot"
)

// CaptureDirect grabs the primary display of the CURRENT session as PNG. On a
// session-0 Windows service this fails ("no active displays"); the Windows
// ScreenCapturer bridges into the active session instead (see
// capture_windows.go). The capture-helper subcommand calls this directly.
func CaptureDirect() ([]byte, error) {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, fmt.Errorf("no active displays")
	}
	img, err := screenshot.CaptureDisplay(0)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
