package ui

import (
	"fmt"
	"path/filepath"
)

// titlePushed records that the terminal's own title was saved, so it is only
// saved once however many times the title changes afterwards.
var titlePushed bool

// SetTerminalTitle names the window crema is running in. Two mechanisms, since
// no single one covers both hosts: the OSC sequence every VT terminal
// understands — Windows Terminal shows it on the tab — and, on Windows, the
// console API, which is what a classic console window puts in its title bar
// and therefore on its taskbar button.
//
// The title is also the only part of a terminal's window that a guest program
// can name at all. Inside Windows Terminal the window belongs to the terminal,
// so this is what tells the taskbar and alt-tab that the thing running is
// crema; a window of crema's own needs a classic console host, which
// scripts/shortcut.ps1 sets up.
func SetTerminalTitle(title string) {
	if stdoutIsTerminal() {
		if !titlePushed {
			fmt.Fprint(bgWriter, "\x1b[22;0t") // save the terminal's own title
			titlePushed = true
		}
		fmt.Fprintf(bgWriter, "\x1b]0;%s\x07", title)
	}
	setConsoleTitle(title)
	adoptConsoleWindow()
}

// RestoreTerminalTitle hands the title back on the way out, so the shell
// doesn't keep crema's name after it exits.
func RestoreTerminalTitle() {
	if titlePushed && stdoutIsTerminal() {
		fmt.Fprint(bgWriter, "\x1b[23;0t")
		titlePushed = false
	}
}

// WindowTitle is what crema calls itself: the product plus the folder it was
// opened on, which is what tells two cremas apart in the taskbar.
func WindowTitle(dir string) string {
	if base := filepath.Base(dir); base != "" && base != "." && base != string(filepath.Separator) {
		return "Crema — " + base
	}
	return "Crema"
}
