package main

import "os"

// Crema wants a window of its own — its own taskbar button, with its own icon,
// apart from whatever terminal is running. A shortcut arranges that by
// launching crema through conhost.exe, the classic console host. The binary can
// arrange it for itself too, by noticing it has been started on its own into a
// terminal that isn't its, and starting again through conhost.
//
// The catch is that starting from a terminal on purpose — the ordinary way to
// run a command — must keep working, and a second window would be an insult
// there. What tells the two apart is who else is attached to the console: a
// double-click gets a console of its own, while a command typed at a prompt
// shares the shell's.

const (
	windowAuto  = "auto"  // own window when started on its own
	windowOwn   = "own"   // always
	windowShare = "share" // never; stay in the terminal that started crema
)

// relaunchEnv marks the process crema started for itself, so a relaunch can
// never relaunch again however the detection behaves on an odd host.
const relaunchEnv = "CREMA_OWN_WINDOW"

// consoleState is what the OS says about the console crema started in.
type consoleState struct {
	terminal bool // stdout is a terminal rather than a pipe or a file
	console  bool // there is a console window behind it
	owned    bool // ...and it is a real one, on screen, that crema has to itself
	alone    bool // no other process shares the console: nobody typed a command
	child    bool // this process is itself a relaunch
}

// wantsOwnWindow decides whether to start again in a window of crema's own.
func wantsOwnWindow(mode string, st consoleState) bool {
	switch {
	case st.child, !st.terminal, !st.console:
		// Already the relaunch, or output is going somewhere that isn't a
		// terminal, or there is no console at all — a service, a build step.
		return false
	case st.owned:
		return false // this window is crema's; that is the whole goal
	case mode == windowShare:
		return false
	case mode == windowOwn:
		return true
	default:
		return st.alone
	}
}

// ownWindow starts crema again in a console window of its own and reports
// whether it did, in which case this process should quit and leave the new one
// to it.
func ownWindow(mode string) (bool, error) {
	if !wantsOwnWindow(mode, consoleNow()) {
		return false, nil
	}
	if err := startInOwnWindow(); err != nil {
		// Not being able to open a window is no reason to refuse to run: carry
		// on in the terminal that started us.
		return false, err
	}
	return true, nil
}

// relaunched reports whether this process is the one crema started for itself.
func relaunched() bool { return os.Getenv(relaunchEnv) != "" }
