//go:build windows

package ui

import "golang.org/x/sys/windows"

// consoleWindowSize is the console's visible window, in cells — not the
// screen buffer behind it, which is what the resize event reports.
func realConsoleWindowSize() (int, int, bool) {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return 0, 0, false
	}
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(h, &info); err != nil {
		return 0, 0, false
	}
	w := int(info.Window.Right-info.Window.Left) + 1
	ht := int(info.Window.Bottom-info.Window.Top) + 1
	return w, ht, w > 0 && ht > 0
}
