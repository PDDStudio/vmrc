//go:build windows

package agent

import (
	"fmt"
	"io"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// captureHelperTimeout bounds how long a screenshot waits for the session
// helper. A capture normally takes well under a second; a helper stuck behind a
// prompt or a hung display driver must not pin the HTTP request (the server has
// no write timeout by design) forever. A var so tests can shorten it.
var captureHelperTimeout = 30 * time.Second

// captureDrainTimeout bounds how long we wait for the helper's stdout pipe to
// reach EOF after the helper process itself has exited.
const captureDrainTimeout = 5 * time.Second

// pipeResult is what the pipe-reader goroutine reports back.
type pipeResult struct {
	data []byte
	err  error
}

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
	defer func() { _ = userTok.Close() }()

	var primary windows.Token
	if err := windows.DuplicateTokenEx(
		userTok, windows.MAXIMUM_ALLOWED, nil,
		windows.SecurityImpersonation, windows.TokenPrimary, &primary,
	); err != nil {
		return nil, fmt.Errorf("duplicate token: %w", err)
	}
	defer func() { _ = primary.Close() }()

	// Anonymous pipe; the write end is inheritable so the child can use it.
	sa := windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(sa))
	var rd, wr windows.Handle
	if err := windows.CreatePipe(&rd, &wr, &sa, 0); err != nil {
		return nil, fmt.Errorf("create pipe: %w", err)
	}
	// rd has a single owner: this function until it is wrapped in an *os.File
	// and handed to the reader goroutine, which then closes it exactly once (it
	// may outlive this function if a helper is killed). os.NewFile installs a
	// finalizer that would also close the raw handle, so we must not
	// CloseHandle(rd) separately once it has been handed over.
	handedOver := false
	defer func() {
		if !handedOver {
			_ = windows.CloseHandle(rd)
		}
	}()
	// Keep the read end out of the child.
	if err := windows.SetHandleInformation(rd, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		_ = windows.CloseHandle(wr)
		return nil, fmt.Errorf("set handle information: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		_ = windows.CloseHandle(wr)
		return nil, err
	}
	cmdline, err := windows.UTF16PtrFromString(fmt.Sprintf(`"%s" capture-helper`, exe))
	if err != nil {
		_ = windows.CloseHandle(wr)
		return nil, err
	}
	desktop, err := windows.UTF16PtrFromString(`winsta0\default`)
	if err != nil {
		_ = windows.CloseHandle(wr)
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
	_ = windows.CloseHandle(wr) // child has its own copy; parent must close to see EOF
	if err != nil {
		return nil, fmt.Errorf("create process as user: %w", err)
	}
	defer func() { _ = windows.CloseHandle(pi.Thread) }()
	defer func() { _ = windows.CloseHandle(pi.Process) }()

	// Drain the PNG from the pipe concurrently (the child blocks once the pipe
	// buffer is full), while this goroutine waits on the process with a bound.
	rf := os.NewFile(uintptr(rd), "capture-pipe")
	handedOver = true
	results := make(chan pipeResult, 1)
	go func() {
		data, rerr := io.ReadAll(rf)
		_ = rf.Close()
		results <- pipeResult{data: data, err: rerr}
	}()

	data, err := awaitHelper(pi.Process, results, captureHelperTimeout)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("capture-helper produced no output")
	}
	return data, nil
}

// awaitHelper waits up to timeout for process to exit and then for its output
// (results), and checks the exit code. A helper that overruns the timeout is
// terminated, so a stuck capture can never block a request indefinitely.
func awaitHelper(process windows.Handle, results <-chan pipeResult, timeout time.Duration) ([]byte, error) {
	ev, err := windows.WaitForSingleObject(process, uint32(timeout/time.Millisecond))
	if err != nil {
		_ = windows.TerminateProcess(process, 1)
		return nil, fmt.Errorf("wait helper: %w", err)
	}
	if ev == uint32(windows.WAIT_TIMEOUT) {
		_ = windows.TerminateProcess(process, 1)
		// Best-effort reap so the kill has landed before we report it.
		_, _ = windows.WaitForSingleObject(process, uint32(captureDrainTimeout/time.Millisecond))
		return nil, fmt.Errorf("capture-helper did not finish within %s; terminated it", timeout)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(process, &code); err != nil {
		return nil, fmt.Errorf("helper exit code: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("capture-helper exited with code %d", code)
	}
	var res pipeResult
	select {
	case res = <-results:
	case <-time.After(captureDrainTimeout):
		return nil, fmt.Errorf("capture-helper exited but its output pipe did not close")
	}
	if res.err != nil {
		return nil, fmt.Errorf("read helper output: %w", res.err)
	}
	return res.data, nil
}
