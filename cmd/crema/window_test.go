package main

import "testing"

// The whole point is that a command typed at a prompt stays in that terminal,
// while a crema started on its own gets a window — and a relaunch never
// relaunches again.
func TestWantsOwnWindow(t *testing.T) {
	// started from a shell inside Windows Terminal: the console is shared
	fromShell := consoleState{terminal: true, console: true}
	// double-clicked into Windows Terminal: nobody else on the console, and
	// the window on screen belongs to the terminal
	onItsOwn := consoleState{terminal: true, console: true, alone: true}
	// already in a window of its own, whether by shortcut or by relaunch
	ownsWindow := consoleState{terminal: true, console: true, alone: true, owned: true}

	cases := []struct {
		name string
		mode string
		st   consoleState
		want bool
	}{
		{"started on its own", windowAuto, onItsOwn, true},
		{"typed at a prompt", windowAuto, fromShell, false},
		{"already has a window", windowAuto, ownsWindow, false},
		{"the relaunch itself", windowAuto, consoleState{
			terminal: true, console: true, alone: true, child: true}, false},

		{"forced, from a prompt", windowOwn, fromShell, true},
		{"forced, but already has one", windowOwn, ownsWindow, false},
		{"refused, on its own", windowShare, onItsOwn, false},

		{"output is a pipe", windowAuto, consoleState{console: true, alone: true}, false},
		{"no console at all", windowAuto, consoleState{terminal: true, alone: true}, false},
		{"forced with no console", windowOwn, consoleState{terminal: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wantsOwnWindow(c.mode, c.st); got != c.want {
				t.Fatalf("wantsOwnWindow(%q, %+v) = %v, want %v", c.mode, c.st, got, c.want)
			}
		})
	}
}
