//go:build windows

package agent

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procProcessIdToSessionId = modkernel32.NewProc("ProcessIdToSessionId")
)

// ScreenCapturer captures the desktop. When the agent runs in the active
// console session it captures directly; when it runs as a session-0 service it
// bridges into the active session (see capture_session_windows.go).
type ScreenCapturer struct{}

func (ScreenCapturer) Capture() ([]byte, error) {
	if inActiveSession() {
		return CaptureDirect()
	}
	return captureViaActiveSession()
}

// currentSessionID returns the Windows session the agent process runs in.
func currentSessionID() (uint32, bool) {
	pid := windows.GetCurrentProcessId()
	var sess uint32
	r1, _, _ := procProcessIdToSessionId.Call(uintptr(pid), uintptr(unsafe.Pointer(&sess)))
	if r1 == 0 {
		return 0, false
	}
	return sess, true
}

// inActiveSession reports whether the agent already runs in the active console
// session (so direct capture works). A session-0 LocalSystem service is not in
// the active console session, so this returns false and the bridge is used.
func inActiveSession() bool {
	active := windows.WTSGetActiveConsoleSessionId()
	if active == 0xFFFFFFFF { // no console session attached
		return false
	}
	cur, ok := currentSessionID()
	if !ok {
		return false
	}
	return cur == active
}
