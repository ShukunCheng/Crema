package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The CLI shows a subagent by opening its transcript: enter on its row and
// the pane is that agent's conversation, with the prompt it was handed at the
// top and everything it has done since underneath, live. crema shows it the
// same way, and reads it from the same place the CLI does — the transcript
// file the subagent is writing as it goes.
//
// What this replaces is worth saying. Until now the only way to find out what
// a subagent was doing was to ask the main agent to relay it, which spends a
// whole turn of the most expensive conversation on the machine and only works
// if the model chooses to answer. Reading the file costs nothing, arrives
// while the work is happening rather than after it, and cannot be paraphrased
// wrong.

// subagentView is one subagent's transcript, as crema has read it so far.
type subagentView struct {
	task string
	log  *agent.SubagentLog
	tl   *Timeline
	// misses counts how many times the file has been looked for and not
	// found. The CLI creates it a moment after announcing the subagent, so
	// the first few are expected; past that it is not coming.
	misses int
	// done is set once the task has ended and its file has been read out, so
	// a finished subagent is not stat'd on every tick for the rest of the run.
	done bool
}

// subPollEvery is how often an open transcript is re-read. Fast enough to
// read as live, slow enough that tailing five files is nothing: each poll is
// a stat and, when the file has grown, a read of only the new bytes.
const subPollEvery = 500 * time.Millisecond

// subTickMsg drives the polling. It only re-arms while there is something to
// poll, so an idle crema goes back to its ten-second heartbeat.
type subTickMsg struct{}

func subTick() tea.Cmd {
	return tea.Tick(subPollEvery, func(time.Time) tea.Msg { return subTickMsg{} })
}

// ensureSubView makes sure a subagent has a view to fill, so the next poll
// reads it — even one that finished before it was ever looked at, which the
// poll would otherwise leave alone as nothing to catch up on.
func (s *Session) ensureSubView(id string) {
	if _, ok := s.subs[id]; ok || id == "" {
		return
	}
	if s.subs == nil {
		s.subs = map[string]*subagentView{}
	}
	s.subs[id] = &subagentView{task: id, tl: NewTimeline(s.tl.Width(), s.tl.Height())}
}

// subOpenTries is how long to keep looking for a transcript that has not
// appeared. At the poll cadence this is a few seconds, which is longer than
// the gap measured between a task_started line and its file.
const subOpenTries = 40

// Reading the transcripts happens off the interface's goroutine. It is file
// work — a stat, a read of whatever is new, and for a transcript not yet on
// disk a glob across every project directory — and it runs twice a second
// while anything is working. Measured on this machine, that glob alone took a
// mean of 44 ms and up to 140 ms, and it was repeated for every subagent whose
// file had not appeared yet: during a fan-out of five or six, a few hundred
// milliseconds of every tick in which the keyboard did not answer.
//
// So the tick is split in three. What to read is decided on the interface's
// goroutine, where the sessions live; the reading is done on another; and what
// came back is applied on the interface's goroutine again. Only one poll is in
// flight at a time, so a transcript's read position is only ever moved by one
// reader.

// subWork is one transcript to read, with everything the reader needs copied
// in so it never has to look at a session.
type subWork struct {
	sess      int
	task      string
	dir, sid  string
	log       *agent.SubagentLog // nil until the file has been found
	wasActive bool               // still running when the work was set
}

// subResult is what one read came to.
type subResult struct {
	sess      int
	task      string
	opened    *agent.SubagentLog // set when this read found the file
	evs       []agent.Event
	miss      bool // looked for the file and it was not there yet
	read      bool
	wasActive bool
}

// subPolledMsg carries a finished poll back to the interface.
type subPolledMsg struct{ results []subResult }

// subWorkList is what this session wants read, decided where the session lives.
func (s *Session) subWorkList() []subWork {
	var out []subWork
	for _, t := range s.tasks {
		if t.Type == "local_bash" {
			continue // a shell has no transcript; its output goes to a file
		}
		v, ok := s.subs[t.ID]
		if !ok {
			if t.Status != "running" {
				continue // never opened and already over: nothing to catch up on
			}
			s.ensureSubView(t.ID)
			v = s.subs[t.ID]
		}
		if v.done || (v.log == nil && v.misses > subOpenTries) {
			continue
		}
		out = append(out, subWork{sess: s.ID, task: t.ID, dir: s.Dir, sid: s.agentSID,
			log: v.log, wasActive: t.Status == "running"})
	}
	return out
}

// readSubWork does the file work. It touches no session, so it can run on any
// goroutine.
func readSubWork(work []subWork) []subResult {
	out := make([]subResult, 0, len(work))
	for _, w := range work {
		r := subResult{sess: w.sess, task: w.task, wasActive: w.wasActive}
		l := w.log
		if l == nil {
			opened, err := agent.OpenSubagentLog(w.dir, w.sid, w.task)
			if err != nil {
				r.miss = true
				out = append(out, r)
				continue
			}
			l, r.opened = opened, opened
		}
		evs, err := l.Read()
		r.evs, r.read = evs, err == nil
		out = append(out, r)
	}
	return out
}

// applySub puts what came back into this session's transcripts, and reports
// whether anything new arrived.
func (s *Session) applySub(results []subResult) bool {
	changed := false
	for _, r := range results {
		if r.sess != s.ID {
			continue
		}
		v, ok := s.subs[r.task]
		if !ok {
			continue // the agent was cleared while the read was out
		}
		if r.miss {
			v.misses++
			continue
		}
		if r.opened != nil && v.log == nil {
			v.log, v.misses = r.opened, 0
		}
		for _, ev := range r.evs {
			v.tl.AppendEvent(ev)
		}
		changed = changed || len(r.evs) > 0
		// Only a task that had already stopped when this read was set is done:
		// one that stopped during the read may have written its last lines
		// after it, and those are the answer.
		if r.read && !r.wasActive {
			v.done = true
		}
	}
	return changed
}

// pollSubagents reads and applies in one go, on the calling goroutine. The
// interface does not use it — it polls in the background — but a test wants
// the answer now.
func (s *Session) pollSubagents() bool {
	return s.applySub(readSubWork(s.subWorkList()))
}

// subagentPending reports whether anything is worth polling: a subagent still
// running, or one being read whose file has not caught up yet.
func (s *Session) subagentPending() bool {
	for _, t := range s.tasks {
		if t.Status == "running" && t.Type != "local_bash" {
			return true
		}
		// A row being kept for its last seconds needs a frame to take it
		// away again, or it sits there until the next heartbeat.
		if !t.Ended.IsZero() && time.Since(t.Ended) <= taskLinger && !endedWell(t.Status) {
			return true
		}
	}
	for _, v := range s.subs {
		if !v.done {
			return true
		}
	}
	return false
}

// subHeader is the line above a subagent's transcript saying whose it is and
// how to get out of it. Without it the pane is a conversation with no
// indication that it is not the one you were in.
func (s *Session) subHeader(id string, w int) string {
	t := s.task(id)
	name, what := "subagent", ""
	if t != nil {
		name, what = t.Type, descLine(t.Desc)
		if name == "local_bash" {
			name = "shell"
		}
	}
	if v := s.subs[id]; v != nil && v.log != nil {
		if v.log.Meta.Type != "" {
			name = v.log.Meta.Type
		}
		if what == "" {
			what = descLine(v.log.Meta.Desc)
		}
	}
	state := "running"
	if t != nil && t.Status != "running" {
		state = t.Status
	}
	left := "▸ " + name
	if what != "" {
		left += " · " + what
	}
	right := state + " · esc to go back"
	if n := w - lipgloss.Width(left) - lipgloss.Width(right) - 2; n > 0 {
		return base().Foreground(T.Purple).Bold(true).Render(" "+left) +
			base().Render(strings.Repeat(" ", n)) +
			base().Foreground(T.Muted).Render(right+" ")
	}
	return base().Foreground(T.Purple).Bold(true).Render(" " + clip(left, max(1, w-2)))
}

// task finds one of this session's background tasks by the CLI's id.
func (s *Session) task(id string) *agent.TaskUpdate {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			return &s.tasks[i]
		}
	}
	return nil
}

// waitingForTranscript is what the pane says while the file has not appeared.
func waitingForTranscript(id string) string {
	return fmt.Sprintf("no transcript for %s yet — the CLI writes one a moment "+
		"after a subagent starts, and crema is watching for it", id)
}
