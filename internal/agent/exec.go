package agent

// cmdShellLine builds the raw cmd.exe command line for a remote command. With
// /s, cmd strips exactly the first and last quote of the /c argument and runs
// the remainder unchanged, so the caller's own quoting survives no matter how
// many quotes the line contains. Pure (no build tag) so it is unit-tested on
// every OS; only exec_windows.go uses it.
func cmdShellLine(cmdline string) string {
	return `cmd.exe /s /c "` + cmdline + `"`
}
