//go:build windows || darwin || linux

package agent

import (
	"bytes"
	"fmt"
	"image/png"

	"github.com/kbinani/screenshot"
)

// ScreenCapturer captures the primary display as PNG.
type ScreenCapturer struct{}

func (ScreenCapturer) Capture() ([]byte, error) {
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
