//go:build !windows

package ui

// The console title API and the window-icon message are Windows-only. Every
// other terminal crema runs in takes the OSC sequence and draws its own
// window, icon included.
func setConsoleTitle(string) {}
func adoptConsoleWindow()    {}
