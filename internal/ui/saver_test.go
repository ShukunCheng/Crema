package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The save used to run on the interface's own goroutine — the one that reads
// your keys — and against this machine's real state, eight agents and 2.7 MB,
// it took up to 219 ms before Windows had even looked at the file. persist now
// hands a copy to a writer and returns, so the keyboard never waits on a disk.
func TestSavingDoesNotHoldUpTheKeyboard(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	// A conversation big enough that writing it takes real time.
	big := strings.Repeat("a line of tool output that goes on for a while\n", 400)
	blocks := make([]Block, 300)
	for i := range blocks {
		blocks[i] = Block{Kind: BlockToolOutput, Text: big}
	}
	s.tl.Restore(blocks) // one render, rather than three hundred appends
	t0 := time.Now()
	a.persist()
	if d := time.Since(t0); d > 50*time.Millisecond {
		t.Fatalf("persist took %s on the interface's goroutine; it should hand off and return", d)
	}
	FlushState()
	if b, err := os.ReadFile(StatePath()); err != nil || len(b) == 0 {
		t.Fatalf("and the save should still happen: %v", err)
	}
}

// A save goes where it was asked to go at the moment it was asked, not
// wherever the path happens to point by the time the writer gets to it.
func TestASaveGoesWhereItWasAskedToGo(t *testing.T) {
	a := testApp(t)
	first := StatePath()
	a.persist()

	other := filepath.Join(t.TempDir(), "state.json")
	prev := statePathOverride
	statePathOverride = other
	t.Cleanup(func() { statePathOverride = prev })

	FlushState()
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("the save should be in the file it was meant for: %v", err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Fatal("and nowhere else")
	}
}

// Reading the state sees everything already asked to be saved; a sync save
// waits for queued ones, so an older state can never land on top of it.
func TestLoadingSeesWhatWasSaved(t *testing.T) {
	a := testApp(t)
	a.cur().Name = "the one I just renamed"
	a.persist()
	st := LoadState() // no flush by hand: loading waits for it
	if len(st.Sessions) == 0 || st.Sessions[0].Name != "the one I just renamed" {
		t.Fatalf("loading should see the save that was queued: %+v", st.Sessions)
	}
}

// Saves asked for faster than they can be written collapse to the newest.
func TestABurstOfSavesEndsWithTheNewest(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 50; i++ {
		a.cur().Name = "name " + itoa(i)
		a.persist()
	}
	if got := LoadState().Sessions[0].Name; got != "name 49" {
		t.Fatalf("the file should hold the last state asked for, got %q", got)
	}
}
