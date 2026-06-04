package agent

import (
	"fmt"
	"strings"
)

// Injector performs synthetic mouse and keyboard input on the local desktop.
// The real implementation lives in the platform build-tagged files
// (input_windows.go today; input_darwin.go / input_linux.go later); other
// platforms get a stub that reports the feature unsupported. Tests inject a
// fake. Mirrors the Capturer pattern.
type Injector interface {
	// Click moves the cursor to absolute screen pixel (x,y) and clicks button
	// ("left", "right", "middle"); double issues a double-click.
	Click(x, y int, button string, double bool) error
	// Type enters text as literal Unicode keystrokes (layout-independent).
	Type(text string) error
	// Key presses a key combo such as "Enter", "Ctrl+C", "Ctrl+Shift+Esc".
	Key(combo string) error
}

// Windows virtual-key codes. Defined here (not in input_windows.go) so the
// combo parser and its tests build on every platform. Letters A-Z and digits
// 0-9 are omitted: their VK codes equal their uppercase ASCII byte and are
// derived directly in vkForToken.
const (
	vkBack   = 0x08
	vkTab    = 0x09
	vkReturn = 0x0D
	vkShift  = 0x10
	vkCtrl   = 0x11
	vkAlt    = 0x12 // VK_MENU
	vkEscape = 0x1B
	vkSpace  = 0x20
	vkPrior  = 0x21 // Page Up
	vkNext   = 0x22 // Page Down
	vkEnd    = 0x23
	vkHome   = 0x24
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
	vkInsert = 0x2D
	vkDelete = 0x2E
	vkLWin   = 0x5B
	vkF1     = 0x70 // F2..F12 are sequential from here
)

// vkByName maps a (lower-cased) named key token to its virtual-key code.
var vkByName = map[string]uint16{
	"enter": vkReturn, "return": vkReturn,
	"tab":       vkTab,
	"esc":       vkEscape,
	"escape":    vkEscape,
	"space":     vkSpace,
	"backspace": vkBack,
	"back":      vkBack,
	"delete":    vkDelete,
	"del":       vkDelete,
	"insert":    vkInsert,
	"ins":       vkInsert,
	"home":      vkHome,
	"end":       vkEnd,
	"pageup":    vkPrior,
	"pgup":      vkPrior,
	"pagedown":  vkNext,
	"pgdn":      vkNext,
	"up":        vkUp,
	"down":      vkDown,
	"left":      vkLeft,
	"right":     vkRight,
	"f1":        vkF1, "f2": vkF1 + 1, "f3": vkF1 + 2, "f4": vkF1 + 3,
	"f5": vkF1 + 4, "f6": vkF1 + 5, "f7": vkF1 + 6, "f8": vkF1 + 7,
	"f9": vkF1 + 8, "f10": vkF1 + 9, "f11": vkF1 + 10, "f12": vkF1 + 11,
}

// modByName maps a (lower-cased) modifier token to its virtual-key code.
var modByName = map[string]uint16{
	"ctrl": vkCtrl, "control": vkCtrl,
	"shift": vkShift,
	"alt":   vkAlt,
	"win":   vkLWin, "meta": vkLWin, "super": vkLWin, "cmd": vkLWin,
}

// vkForToken resolves a single non-modifier key token to a virtual-key code.
// Named keys come from vkByName; single ASCII letters/digits map to their
// uppercase-ASCII VK code.
func vkForToken(tok string) (uint16, error) {
	t := strings.ToLower(strings.TrimSpace(tok))
	if vk, ok := vkByName[t]; ok {
		return vk, nil
	}
	if len(t) == 1 {
		c := t[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint16(c - 'a' + 'A'), nil
		case c >= '0' && c <= '9':
			return uint16(c), nil
		}
	}
	return 0, fmt.Errorf("unknown key %q", tok)
}

// parseCombo splits a combo like "Ctrl+Shift+Esc" into its modifier VK codes
// (in order) and the final key VK code. Tokens are separated by '+'; the last
// token is the key, the rest are modifiers. Platform-neutral so it is unit
// tested without a real desktop.
func parseCombo(combo string) (mods []uint16, key uint16, err error) {
	parts := strings.Split(combo, "+")
	if len(parts) == 0 || strings.TrimSpace(parts[len(parts)-1]) == "" {
		return nil, 0, fmt.Errorf("empty key combo %q", combo)
	}
	for _, p := range parts[:len(parts)-1] {
		vk, ok := modByName[strings.ToLower(strings.TrimSpace(p))]
		if !ok {
			return nil, 0, fmt.Errorf("unknown modifier %q in %q", strings.TrimSpace(p), combo)
		}
		mods = append(mods, vk)
	}
	key, err = vkForToken(parts[len(parts)-1])
	if err != nil {
		return nil, 0, err
	}
	return mods, key, nil
}
