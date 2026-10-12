package ui

import (
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeConsole is a window whose size the test sets.
type fakeConsole struct {
	mu   sync.Mutex
	w, h int
}

func (f *fakeConsole) size() (int, int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.w, f.h, true
}

func (f *fakeConsole) set(w, h int) {
	f.mu.Lock()
	f.w, f.h = w, h
	f.mu.Unlock()
}

func watchFake(t *testing.T, f *fakeConsole) chan tea.Msg {
	t.Helper()
	prevSize, prevEvery := consoleWindowSize, sizeWatchEvery
	consoleWindowSize, sizeWatchEvery = f.size, 5*time.Millisecond
	got := make(chan tea.Msg, 16)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { WatchConsoleSize(func(m tea.Msg) { got <- m }, stop); close(done) }()
	t.Cleanup(func() {
		close(stop)
		<-done
		consoleWindowSize, sizeWatchEvery = prevSize, prevEvery
	})
	return got
}

// A window that comes back to full size without crema hearing about it is
// found and put right: the watcher sends the real size, and the app turns it
// into an ordinary resize that the renderer hears too.
func TestAMissedResizeIsCaught(t *testing.T) {
	a := testApp(t)
	a.resize(52, 13) // the small window crema last heard about
	f := &fakeConsole{w: 159, h: 47}
	got := watchFake(t, f)

	var msg tea.Msg
	select {
	case msg = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the window and the layout disagree; the watcher should have said so")
	}
	_, cmd := a.Update(msg)
	if cmd == nil {
		t.Fatal("the correction should come back as a resize")
	}
	ws, ok := cmd().(tea.WindowSizeMsg)
	if !ok || ws.Width != 159 || ws.Height != 47 {
		t.Fatalf("resize = %#v, want the window's real 159x47", ws)
	}
	a.Update(ws)
	if a.w != 159 || a.h != 47 {
		t.Fatalf("layout is %dx%d, want 159x47", a.w, a.h)
	}
}

// While the window and the layout agree, the watcher says nothing: an idle
// crema is not woken four times a second for no reason.
func TestTheWatcherIsQuietWhileTheSizesAgree(t *testing.T) {
	a := testApp(t)
	a.resize(120, 30)
	got := watchFake(t, &fakeConsole{w: 120, h: 30})
	select {
	case m := <-got:
		t.Fatalf("nothing is wrong, yet it sent %#v", m)
	case <-time.After(100 * time.Millisecond):
	}
}
