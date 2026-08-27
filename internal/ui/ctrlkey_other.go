//go:build !windows

package ui

// ctrlDown is only needed on Windows, whose console reader drops the
// control-key state for backspace. Everywhere else the terminal sends ^H for
// ctrl+backspace and the key speaks for itself.
func ctrlDown() bool { return false }

// enterDown is likewise a Windows question: there, a paste is replayed as key
// events and has to be told apart from a keypress. Every other terminal
// crema runs in brackets its pastes, so an enter here is always a real one.
func enterDown() bool { return true }

// shiftDown is a Windows question too: elsewhere a terminal that can tell
// shift+enter from enter says so in the key itself.
func shiftDown() bool { return false }
