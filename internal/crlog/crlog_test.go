package crlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fresh(t *testing.T) string {
	t.Helper()
	Close()
	mu.Lock()
	path, ch, done, dropped = "", nil, nil, 0
	mu.Unlock()
	dir := t.TempDir()
	Open(dir)
	t.Cleanup(Close)
	return dir
}

// What crema decided, when, and why — the thing that was missing every time a
// turn stopped and there was nothing to point at.
func TestItRecordsWhatHappened(t *testing.T) {
	dir := fresh(t)
	Log("conv.close", "agent", 2, "why", "the model was changed", "busy", true)
	Log("turn.end", "agent", 2, "secs", 91, "cut_short", true)
	Close()

	b, err := os.ReadFile(filepath.Join(dir, "crema.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2:\n%s", len(lines), b)
	}
	var e struct {
		At    string         `json:"t"`
		Event string         `json:"ev"`
		Data  map[string]any `json:"d"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("a line should be readable JSON: %v\n%s", err, lines[0])
	}
	if e.Event != "conv.close" || e.At == "" {
		t.Fatalf("entry = %+v", e)
	}
	if e.Data["why"] != "the model was changed" {
		t.Fatalf("the reason is the whole point: %+v", e.Data)
	}
}

// It must never be a reason crema stops working: no config directory, no log,
// and everything else carries on.
func TestLoggingNowhereIsHarmless(t *testing.T) {
	Close()
	mu.Lock()
	path, ch, done = "", nil, nil
	mu.Unlock()
	Log("turn.start", "agent", 1) // must not panic or block
	if got := Path(); got != "" {
		t.Fatalf("path = %q, want none", got)
	}
	if got := Tail(10); got != nil {
		t.Fatalf("tail = %v, want nothing", got)
	}
}

// Tail is how /log reads it back without another program.
func TestTailReadsTheEnd(t *testing.T) {
	fresh(t)
	for i := 0; i < 50; i++ {
		Log("turn.end", "n", i)
	}
	Close()
	got := Tail(5)
	if len(got) != 5 {
		t.Fatalf("tail returned %d lines, want 5", len(got))
	}
	if !strings.Contains(got[4], `"n":49`) {
		t.Fatalf("the last line should be the newest: %q", got[4])
	}
	if !strings.Contains(got[0], `"n":45`) {
		t.Fatalf("and oldest first: %q", got[0])
	}
}

// A frame must never wait on a disk. A full queue drops the line and counts
// it, so a gap in the file is never a silent one.
func TestABusyFrameIsNeverBlocked(t *testing.T) {
	Close()
	mu.Lock()
	// A writer that is never started, so nothing drains the queue.
	path, ch, done, dropped = "nowhere", make(chan entry, 2), nil, 0
	mu.Unlock()
	for i := 0; i < 50; i++ {
		Log("turn.start", "n", i) // would deadlock if it waited
	}
	if Dropped() == 0 {
		t.Fatal("a full queue should drop and count, not block")
	}
	mu.Lock()
	ch, path = nil, ""
	mu.Unlock()
}
