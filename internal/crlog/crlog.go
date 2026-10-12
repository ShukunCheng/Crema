// Package crlog is crema's own diagnostic log: what crema decided, when, and
// why.
//
// It exists because three bugs in a row were unanswerable after the fact. A
// subagent was killed and nothing recorded what killed it. A turn stopped
// before finishing and the branch that handled it said nothing at all. The CLI
// printed "Failed to authenticate" to stderr and exited, and the one sentence
// that explained the failure was dropped on the floor. In each case the
// information existed for a moment and was thrown away.
//
// It costs no tokens. Tokens are spent on API calls; this is a local file and
// nothing in it is ever sent to a model. The only way a log can cost anything
// is if someone pastes it into a prompt, which is why this one records what
// crema did rather than what was said — the conversations are already on disk
// under ~/.claude/projects, and duplicating them here would make a file too
// big to read and too private to share.
package crlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxBytes is how large the log grows before it is rolled over. Small enough
// to open in an editor, large enough to hold a long day.
const maxBytes = 4 << 20

// queue is how many lines may be waiting to be written. A frame must never
// wait on a disk, so a full queue drops the line and says so rather than
// blocking the interface it exists to explain.
const queue = 512

type entry struct {
	At    string         `json:"t"`
	Event string         `json:"ev"`
	Data  map[string]any `json:"d,omitempty"`
}

var (
	mu      sync.Mutex
	path    string
	ch      chan entry
	done    chan struct{}
	dropped int
)

// Path is where the log is written, empty when it is not open.
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return path
}

// Open starts the log in dir. It is best-effort: a log that cannot be written
// is not a reason to refuse to run, so a failure here leaves logging off and
// everything else working.
func Open(dir string) {
	mu.Lock()
	defer mu.Unlock()
	if ch != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path = filepath.Join(dir, "crema.log")
	ch, done = make(chan entry, queue), make(chan struct{})
	go write(path, ch, done)
}

// Close flushes what is queued and stops the writer. Called on the way out, so
// the last thing that happened is in the file.
func Close() {
	mu.Lock()
	c, d := ch, done
	ch, done = nil, nil
	mu.Unlock()
	if c == nil {
		return
	}
	close(c)
	<-d
}

// Log records one thing that happened. The pairs are a flat key/value list —
// Log("conv.close", "agent", 2, "why", "the model was changed") — because at
// the call site the point is to say what happened, not to build a struct.
func Log(event string, kv ...any) {
	mu.Lock()
	c := ch
	mu.Unlock()
	if c == nil {
		return
	}
	e := entry{At: time.Now().Format(time.RFC3339Nano), Event: event}
	if len(kv) > 0 {
		e.Data = make(map[string]any, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			e.Data[fmt.Sprint(kv[i])] = kv[i+1]
		}
	}
	select {
	case c <- e:
	default:
		mu.Lock()
		dropped++
		mu.Unlock()
	}
}

// write is the only goroutine that touches the file, so the interface never
// waits on a disk.
func write(p string, in <-chan entry, fin chan<- struct{}) {
	defer close(fin)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	size := int64(0)
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}
	enc := json.NewEncoder(f)
	for e := range in {
		if size > maxBytes {
			// Roll over: one old file is kept, so the log of the run before
			// this one survives a busy day.
			f.Close()
			_ = os.Rename(p, p+".1")
			nf, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return
			}
			f, enc, size = nf, json.NewEncoder(nf), 0
		}
		before, _ := f.Seek(0, 1)
		if err := enc.Encode(e); err != nil {
			return
		}
		if after, _ := f.Seek(0, 1); after > before {
			size = after
		}
	}
}

// Tail is the last n lines, oldest first, for /log. Reading the file back is
// the point: the answer to "why did that stop" is in it, and it should not
// need another program to get at.
func Tail(n int) []string {
	p := Path()
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// Dropped is how many lines were lost to a full queue, which is worth knowing
// before trusting a gap in the file.
func Dropped() int {
	mu.Lock()
	defer mu.Unlock()
	return dropped
}
