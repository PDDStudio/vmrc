//go:build windows

package agent

import (
	"fmt"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// typeKeyDelay is paused between successive characters in Type. Injecting an
// entire string back-to-back (whether as one SendInput batch or rapid per-rune
// calls) outruns the foreground window's input processing: past a word boundary
// keystrokes collapse onto the last character (e.g. "vmrc input" -> "vmrc nnn").
// A small per-key delay paces injection to what apps reliably consume. 12ms
// removes the gross failure but still glitches a stray character under load;
// 25ms types ~40 chars/sec and is reliable in testing.
const typeKeyDelay = 25 * time.Millisecond

var (
	modUser32            = windows.NewLazySystemDLL("user32.dll")
	procSendInput        = modUser32.NewProc("SendInput")
	procGetSystemMetrics = modUser32.NewProc("GetSystemMetrics")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	// MOUSEINPUT dwFlags.
	mouseeventfMove        = 0x0001
	mouseeventfLeftDown    = 0x0002
	mouseeventfLeftUp      = 0x0004
	mouseeventfRightDown   = 0x0008
	mouseeventfRightUp     = 0x0010
	mouseeventfMiddleDown  = 0x0020
	mouseeventfMiddleUp    = 0x0040
	mouseeventfVirtualDesk = 0x4000
	mouseeventfAbsolute    = 0x8000

	// KEYBDINPUT dwFlags.
	keyeventfKeyUp   = 0x0002
	keyeventfUnicode = 0x0004

	// GetSystemMetrics indices for the virtual desktop (all monitors).
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

// mouseInput mirrors Win32 MOUSEINPUT. On amd64 it is 24 bytes: the trailing
// dwExtraInfo (ULONG_PTR) forces 8-byte alignment, padding the struct after the
// five 32-bit fields.
type mouseInput struct {
	dx          int32
	dy          int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// keybdInput mirrors Win32 KEYBDINPUT; also 24 bytes on amd64, so it fits the
// same union slot as mouseInput.
type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// input mirrors Win32 INPUT. Go has no unions, so the largest member
// (mouseInput) backs the union and keyboard events are written through an
// unsafe cast to *keybdInput. The explicit padding after typ reproduces the
// 4-byte gap the C compiler inserts to 8-align the union member. The size
// passed to SendInput is unsafe.Sizeof(input{}), never a hardcoded constant —
// SendInput rejects a mismatched cbSize, which doubles as a layout self-check.
type input struct {
	typ uint32
	_   uint32
	u   mouseInput
}

var inputSize = uint32(unsafe.Sizeof(input{}))

// SystemInjector injects input via the Win32 SendInput API. It only works from
// the active console session: a session-0 service injects into an invisible
// desktop, so we refuse with ErrNoInteractiveSession (mirroring the screenshot
// path) rather than silently doing nothing.
type SystemInjector struct{}

func sendInputs(events []input) error {
	if len(events) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(
		uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])),
		uintptr(inputSize),
	)
	// SendInput returns the count of events inserted; a short count means input
	// was blocked (UIPI, a foreground lock) and err carries the last OS error.
	if int(n) != len(events) {
		return fmt.Errorf("SendInput inserted %d/%d events: %w", n, len(events), err)
	}
	return nil
}

func getSystemMetric(index int32) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(r)
}

// toAbsolute maps a screen pixel to the 0..65535 normalized coordinate space
// SendInput uses with MOUSEEVENTF_ABSOLUTE|VIRTUALDESK, spanning the whole
// virtual desktop. The (W-1) divisor (not W) maps the last pixel column to
// exactly 65535; rounding minimizes off-by-one landing error.
func toAbsolute(x, y int) (int32, int32) {
	vx := getSystemMetric(smXVirtualScreen)
	vy := getSystemMetric(smYVirtualScreen)
	vw := getSystemMetric(smCXVirtualScreen)
	vh := getSystemMetric(smCYVirtualScreen)
	if vw <= 1 {
		vw = 2
	}
	if vh <= 1 {
		vh = 2
	}
	nx := (int64(int32(x)-vx)*65535 + int64(vw-1)/2) / int64(vw-1)
	ny := (int64(int32(y)-vy)*65535 + int64(vh-1)/2) / int64(vh-1)
	return int32(nx), int32(ny)
}

func mouseEvent(flags uint32, absX, absY int32) input {
	in := input{typ: inputMouse}
	mi := (*mouseInput)(unsafe.Pointer(&in.u))
	mi.dx = absX
	mi.dy = absY
	mi.dwFlags = flags
	return in
}

func keyEvent(vk, scan uint16, flags uint32) input {
	in := input{typ: inputKeyboard}
	ki := (*keybdInput)(unsafe.Pointer(&in.u))
	ki.wVk = vk
	ki.wScan = scan
	ki.dwFlags = flags
	return in
}

// Click moves to (x,y) and clicks. Coordinates are virtual-desktop pixels and
// assume the agent process shares the screenshot helper's DPI awareness
// (system, via the app manifest) and 100% scaling on a single monitor; mixed
// DPI / scaling can offset the landing point.
func (SystemInjector) Click(x, y int, button string, double bool) error {
	if !inActiveSession() {
		return ErrNoInteractiveSession
	}
	var down, up uint32
	switch button {
	case "", "left":
		down, up = mouseeventfLeftDown, mouseeventfLeftUp
	case "right":
		down, up = mouseeventfRightDown, mouseeventfRightUp
	case "middle":
		down, up = mouseeventfMiddleDown, mouseeventfMiddleUp
	default:
		return fmt.Errorf("unknown mouse button %q", button)
	}

	ax, ay := toAbsolute(x, y)
	abs := uint32(mouseeventfAbsolute | mouseeventfVirtualDesk)
	events := []input{
		mouseEvent(mouseeventfMove|abs, ax, ay),
		mouseEvent(down|abs, ax, ay),
		mouseEvent(up|abs, ax, ay),
	}
	if double {
		events = append(events,
			mouseEvent(down|abs, ax, ay),
			mouseEvent(up|abs, ax, ay),
		)
	}
	return sendInputs(events)
}

// Type enters text as Unicode keystrokes. With KEYEVENTF_UNICODE wVk is 0 and
// wScan carries a UTF-16 code unit, so the active keyboard layout is bypassed.
// A literal '\n' is sent as VK_RETURN (a Unicode newline is a no-op in most
// controls); '\r' is dropped to avoid a double Enter on CRLF.
//
// Each rune is sent in its own SendInput call, paced by typeKeyDelay. Both the
// batching and the pacing matter: a single giant batch (or rapid back-to-back
// per-rune calls, which inject just as fast) outrun the foreground window and
// keystrokes past a word boundary collapse onto the last character. The delay
// throttles injection to a rate apps reliably consume. A rune's surrogate pair
// (>BMP) still goes in one call so Windows reassembles it.
func (SystemInjector) Type(text string) error {
	if !inActiveSession() {
		return ErrNoInteractiveSession
	}
	for i, r := range text {
		if i > 0 {
			time.Sleep(typeKeyDelay)
		}
		var events []input
		switch r {
		case '\n':
			events = []input{
				keyEvent(vkReturn, 0, 0),
				keyEvent(vkReturn, 0, keyeventfKeyUp),
			}
		case '\r':
			continue
		default:
			for _, cu := range utf16.Encode([]rune{r}) {
				events = append(events,
					keyEvent(0, cu, keyeventfUnicode),
					keyEvent(0, cu, keyeventfUnicode|keyeventfKeyUp),
				)
			}
		}
		if err := sendInputs(events); err != nil {
			return err
		}
	}
	return nil
}

// Key presses a combo (e.g. "Ctrl+C"). Modifiers go down in order, the key is
// pressed and released, then modifiers release in reverse (LIFO), as a human
// would. Note: Ctrl+Alt+Del is a Secure Attention Sequence the OS intercepts
// and cannot be synthesized via SendInput.
func (SystemInjector) Key(combo string) error {
	if !inActiveSession() {
		return ErrNoInteractiveSession
	}
	mods, key, err := parseCombo(combo)
	if err != nil {
		return err
	}
	var events []input
	for _, m := range mods {
		events = append(events, keyEvent(m, 0, 0))
	}
	events = append(events,
		keyEvent(key, 0, 0),
		keyEvent(key, 0, keyeventfKeyUp),
	)
	for i := len(mods) - 1; i >= 0; i-- {
		events = append(events, keyEvent(mods[i], 0, keyeventfKeyUp))
	}
	return sendInputs(events)
}
