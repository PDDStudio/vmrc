//go:build !windows

package main

// hideOwnConsoleWindow is a no-op on non-Windows platforms.
func hideOwnConsoleWindow() {}
