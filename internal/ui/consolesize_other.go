//go:build !windows

package ui

// consoleWindowSize has nothing to ask elsewhere: SIGWINCH already reports
// the terminal's real size, so the watcher has no gap to close.
func realConsoleWindowSize() (int, int, bool) { return 0, 0, false }
