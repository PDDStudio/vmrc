//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	user32                    = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procShowWindow            = user32.NewProc("ShowWindow")
)

const swHide = 0 // SW_HIDE

// hideOwnConsoleWindow hides this process's console window, but only when the
// process is the sole client of its console (i.e. launched by the logon
// Scheduled Task into its own console). It is a no-op when there is no console
// or when the console is shared with a terminal, so a developer running
// `vmrc-agent run` from a shell keeps their window.
func hideOwnConsoleWindow() {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	var pids [8]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if !ownsConsole(int(n)) {
		return
	}
	_, _, _ = procShowWindow.Call(hwnd, uintptr(swHide))
}
