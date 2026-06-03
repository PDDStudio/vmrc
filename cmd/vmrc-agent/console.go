package main

// ownsConsole reports whether this process is the sole client attached to its
// console (procCount is the number of processes the OS reports attached to the
// console). Only then is it safe to hide the console window: a process launched
// by the logon Scheduled Task gets its own console (count 1), whereas one run
// from a terminal shares it with the shell (count >= 2) and hiding that would
// hide the user's terminal.
func ownsConsole(procCount int) bool { return procCount == 1 }
