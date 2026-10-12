package ui

import (
	"sync"
	"time"

	"github.com/ShukunCheng/Crema/internal/crlog"
)

// Saving used to happen on the interface's own goroutine, and bubbletea has
// only one: the same one that reads your keys and draws the screen. So every
// save was a moment the keyboard stopped answering.
//
// That was cheap once and is not now. Measured against this machine's real
// state file — eight agents, 2.7 MB of JSON — one save took a mean of 113 ms
// and up to 219 ms, before Windows got to scan the freshly written file, and
// it ran at the end of every turn of every agent, twice. With several agents
// working, a keystroke would regularly land behind one, which is what "I type
// and it takes a second to react" was.
//
// Now the interface only takes a copy — cheap, since a block's strings are
// shared rather than duplicated — and a writer goroutine does the marshalling
// and the disk. Saves asked for faster than they can be written collapse into
// one: only the newest state is worth writing, and writing every intermediate
// one would just make the disk busier.

// pendingSave is a state and the file it was meant for. The destination is
// taken when the save is asked for, not when it is written: a save belongs to
// the moment it was requested, and reading the path later let one test's save
// land in another test's file.
type pendingSave struct {
	st   State
	path string
}

type saver struct {
	mu      sync.Mutex
	next    *pendingSave // the newest save waiting to be written, nil when none is
	wake    chan struct{}
	idle    *sync.Cond // signalled when nothing is waiting or being written
	writing bool
	started bool
}

var bg = &saver{wake: make(chan struct{}, 1)}

func init() { bg.idle = sync.NewCond(&bg.mu) }

// saveLater hands a state to the writer and returns at once.
func saveLater(st State) {
	bg.mu.Lock()
	bg.next = &pendingSave{st: st, path: StatePath()}
	if !bg.started {
		bg.started = true
		go bg.run()
	}
	bg.mu.Unlock()
	select {
	case bg.wake <- struct{}{}:
	default: // the writer is already going to look
	}
}

func (s *saver) run() {
	for range s.wake {
		for {
			s.mu.Lock()
			st := s.next
			s.next = nil
			if st == nil {
				s.writing = false
				s.idle.Broadcast()
				s.mu.Unlock()
				break
			}
			s.writing = true
			s.mu.Unlock()

			t0 := time.Now()
			err := writeStateTo(st.path, st.st)
			if d := time.Since(t0); d > 250*time.Millisecond || err != nil {
				// Worth knowing if the disk is the slow part now, since it is
				// no longer in front of the keyboard to be noticed.
				msg := ""
				if err != nil {
					msg = err.Error()
				}
				crlog.Log("state.save", "ms", d.Milliseconds(), "err", msg)
			}
		}
	}
}

// FlushState waits until everything asked for has been written. Called on the
// way out, so quitting never loses the last thing that happened.
func FlushState() {
	bg.mu.Lock()
	defer bg.mu.Unlock()
	for bg.next != nil || bg.writing {
		bg.idle.Wait()
	}
}
