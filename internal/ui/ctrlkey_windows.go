//go:build windows

package ui

import "golang.org/x/sys/windows"

// user32 is shared with window_windows.go, which needs it for the console
// window's icon.
var user32 = windows.NewLazySystemDLL("user32.dll")

var procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")

const (
	vkShift   = 0x10   // VK_SHIFT
	vkControl = 0x11   // VK_CONTROL
	vkReturn  = 0x0D   // VK_RETURN
	keyIsDown = 0x8000 // the high bit of GetAsyncKeyState's answer
	// keyWasTapped is the low bit: pressed at some point since the last call
	// that asked about this key.
	keyWasTapped = 0x0001
)

// ctrlDown asks the OS whether a control key is physically held. It exists
// because bubbletea's Windows console reader maps the backspace key to a plain
// backspace and discards the control-key state that came with it, so
// ctrl+backspace is otherwise indistinguishable from backspace. The answer is
// read within a keystroke of the event, so it is the state that produced it.
func ctrlDown() bool { return keyDown(vkControl) }

// enterDown reports whether the OS saw the enter key pressed: down right now,
// or down at any point since crema last asked. The second half is what
// survives a slow frame — a quick tap can be over before its event is
// processed, and "is it down at this instant" would say no and quietly turn a
// real send into a newline. A paste sets neither bit: it is replayed into the
// console input buffer without going near the keyboard.
//
// The tapped bit is shared system-wide and documented as unreliable, so it is
// only ever believed in one direction — "someone did press enter" — where
// being wrong means sending what was typed, the least surprising mistake.
func enterDown() bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vkReturn))
	return r&(keyIsDown|keyWasTapped) != 0
}

func keyDown(vk int) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r&keyIsDown != 0
}

// shiftDown reports whether a shift key is physically held, for the same
// reason ctrlDown exists: the console reader hands over a plain enter for
// shift+enter as well, with the modifier thrown away.
func shiftDown() bool { return keyDown(vkShift) }
