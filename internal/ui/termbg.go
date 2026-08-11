package ui

import (
	"fmt"
	"io"
	"os"
)

// bgWriter is where the OSC sequences go; tests redirect it.
var bgWriter io.Writer = os.Stdout

// SyncTerminalBackground asks the terminal to adopt the theme's background as
// its default. Crema paints every cell it draws, but the emulator's own window
// padding sits outside the character grid — on Windows Terminal that is an
// 8px frame, which otherwise stays the profile's color and rings a dark theme
// in white. Terminals that don't implement OSC 11 ignore this silently.
func SyncTerminalBackground() {
	if !stdoutIsTerminal() {
		return
	}
	fmt.Fprintf(bgWriter, "\x1b]11;%s\x07", string(T.Bg))
}

// ResetTerminalBackground restores the terminal's own default. Call it on the
// way out, or the user's shell keeps crema's colors after it exits.
func ResetTerminalBackground() {
	if !stdoutIsTerminal() {
		return
	}
	fmt.Fprint(bgWriter, "\x1b]111\x07")
}

// stdoutIsTerminal keeps escape sequences out of pipes and files.
func stdoutIsTerminal() bool {
	f, ok := bgWriter.(*os.File)
	if !ok {
		return true // a test writer; let it observe the sequences
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
