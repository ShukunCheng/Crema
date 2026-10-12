package ui

import (
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The console reports a resize as an input event, and bubbletea relays it as
// the new size. That is the only way crema learns the window changed, and a
// report that goes missing or arrives wrong leaves crema drawing for a window
// that is not there any more: rows spill off the bottom, the status bar and
// the message box vanish, and nothing puts it right until the next resize.
// On Windows the event names the screen buffer's size, not the window's, and
// the two are not promised to agree.
//
// So crema also looks for itself. A goroutine asks the console how big the
// visible window really is, a few times a second, and compares that with the
// size the layout was last built for. Asking is one cheap system call and
// nothing reaches the interface while the two agree, so an idle crema stays
// idle. When they disagree twice running — once could be the ordinary report
// still on its way — it sends the real size as an ordinary resize, which the
// renderer and the layout both take exactly as if the console had said it.

// laidOut is the size the layout was last built for, packed as w<<32|h. It is
// written on the interface's goroutine and read by the watcher.
var laidOut atomic.Uint64

func noteLaidOut(w, h int) { laidOut.Store(uint64(uint32(w))<<32 | uint64(uint32(h))) }

func laidOutSize() (int, int) {
	v := laidOut.Load()
	return int(v >> 32), int(uint32(v))
}

// consoleWindowSize is a seam, so the watcher can be tested without a console
// to resize.
var consoleWindowSize = realConsoleWindowSize

var sizeWatchEvery = 250 * time.Millisecond

// WatchConsoleSize runs until stop closes, sending a resize whenever the
// console's real window and crema's layout have come apart. Where the window
// size cannot be asked for — not a console, not Windows — it returns at once.
func WatchConsoleSize(send func(tea.Msg), stop <-chan struct{}) {
	if _, _, ok := consoleWindowSize(); !ok {
		return
	}
	t := time.NewTicker(sizeWatchEvery)
	defer t.Stop()
	apart := 0
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		w, h, ok := consoleWindowSize()
		lw, lh := laidOutSize()
		if !ok || lw == 0 || w <= 0 || h <= 0 || (w == lw && h == lh) {
			apart = 0
			continue
		}
		if apart++; apart < 2 {
			continue
		}
		apart = 0
		send(sizeCorrectionMsg{tea.WindowSizeMsg{Width: w, Height: h}, lw, lh})
	}
}

// sizeCorrectionMsg is the watcher's resize. It is unwrapped into an ordinary
// WindowSizeMsg by a command, so the renderer sees one too: the renderer only
// takes its size from a WindowSizeMsg itself.
type sizeCorrectionMsg struct {
	real tea.WindowSizeMsg
	oldW int
	oldH int
}
