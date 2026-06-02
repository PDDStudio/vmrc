//go:build windows

package agent

import (
	"fmt"
	"io"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// captureViaActiveSession spawns the hidden `vmrc-agent capture-helper` into the
// active console session (as the logged-in user) and reads the PNG it writes to
// stdout over an anonymous pipe. Used when the agent runs as a session-0 service.
func captureViaActiveSession() ([]byte, error) {
	session := windows.WTSGetActiveConsoleSessionId()
	if session == 0xFFFFFFFF {
		return nil, ErrNoInteractiveSession
	}

	var userTok windows.Token
	if err := windows.WTSQueryUserToken(session, &userTok); err != nil {
		// No user logged into the console session.
		return nil, fmt.Errorf("%w: %v", ErrNoInteractiveSession, err)
	}
	defer userTok.Close()

	var primary windows.Token
	if err := windows.DuplicateTokenEx(
		userTok, windows.MAXIMUM_ALLOWED, nil,
		windows.SecurityImpersonation, windows.TokenPrimary, &primary,
	); err != nil {
		return nil, fmt.Errorf("duplicate token: %w", err)
	}
	defer primary.Close()

	// Anonymous pipe; the write end is inheritable so the child can use it.
	sa := windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(sa))
	var rd, wr windows.Handle
	if err := windows.CreatePipe(&rd, &wr, &sa, 0); err != nil {
		return nil, fmt.Errorf("create pipe: %w", err)
	}
	defer windows.CloseHandle(rd)
	// Keep the read end out of the child.
	if err := windows.SetHandleInformation(rd, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		windows.CloseHandle(wr)
		return nil, fmt.Errorf("set handle information: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		windows.CloseHandle(wr)
		return nil, err
	}
	cmdline, err := windows.UTF16PtrFromString(fmt.Sprintf(`"%s" capture-helper`, exe))
	if err != nil {
		windows.CloseHandle(wr)
		return nil, err
	}
	desktop, err := windows.UTF16PtrFromString(`winsta0\default`)
	if err != nil {
		windows.CloseHandle(wr)
		return nil, err
	}

	si := windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdOutput = wr
	si.Desktop = desktop

	var pi windows.ProcessInformation
	err = windows.CreateProcessAsUser(
		primary, nil, cmdline, nil, nil,
		true, windows.CREATE_NO_WINDOW, nil, nil, &si, &pi,
	)
	windows.CloseHandle(wr) // child has its own copy; parent must close to see EOF
	if err != nil {
		return nil, fmt.Errorf("create process as user: %w", err)
	}
	defer windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)

	// Read the PNG from the pipe (drains as the child writes), then reap it.
	rf := os.NewFile(uintptr(rd), "capture-pipe")
	data, rerr := io.ReadAll(rf)

	if _, err := windows.WaitForSingleObject(pi.Process, windows.INFINITE); err != nil {
		return nil, fmt.Errorf("wait helper: %w", err)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(pi.Process, &code); err != nil {
		return nil, fmt.Errorf("helper exit code: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("capture-helper exited with code %d", code)
	}
	if rerr != nil {
		return nil, fmt.Errorf("read helper output: %w", rerr)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("capture-helper produced no output")
	}
	return data, nil
}
