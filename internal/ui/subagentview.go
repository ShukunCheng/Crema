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

// subagent returns this session's view of one subagent, opening its
// transcript the first time it is asked for. It returns nil while the CLI has
// not written the file yet, which is an ordinary state for the first moment
// of a subagent's life.
func (s *Session) subagent(id string) *subagentView {
	if id == "" {
		return nil
	}
	if v, ok := s.subs[id]; ok {
		if v.log == nil {
			return nil
		}
		return v
	}
	if s.subs == nil {
		s.subs = map[string]*subagentView{}
	}
	v := &subagentView{task: id, tl: NewTimeline(s.tl.Width(), s.tl.Height())}
	s.subs[id] = v
	if !v.open(s) {
		return nil
	}
	return v
}

// open finds the transcript. It is retried on later polls, because a subagent
// is announced on the stream before its file exists.
func (v *subagentView) open(s *Session) bool {
	if v.log != nil {
		return true
	}
	if v.misses > subOpenTries {
		return false
	}
	l, err := agent.OpenSubagentLog(s.Dir, s.agentSID, v.task)
	if err != nil {
		v.misses++
		return false
	}
	v.log, v.misses = l, 0
	return true
}

// subOpenTries is how long to keep looking for a transcript that has not
// appeared. At the poll cadence this is a few seconds, which is longer than
// the gap measured between a task_started line and its file.
const subOpenTries = 40

// pollSubagents reads whatever every subagent of this session has written
// since the last look. It reports whether anything arrived, which is what
// tells the frame it is worth redrawing.
func (s *Session) pollSubagents() bool {
	changed := false
	for _, t := range s.tasks {
		if t.Type == "local_bash" {
			continue // a shell has no transcript; its output goes to a file
		}
		v, ok := s.subs[t.ID]
		if !ok {
			if t.Status != "running" {
				continue // never opened and already over: nothing to catch up on
			}
			if v = s.subagent(t.ID); v == nil {
				continue
			}
		}
		if v.done {
			continue
		}
		if !v.open(s) {
			continue
		}
		evs, err := v.log.Read()
		if err == nil {
			for _, ev := range evs {
				v.tl.AppendEvent(ev)
			}
			changed = changed || len(evs) > 0
		}
		if t.Status != "running" {
			// One last read has just happened, so everything it wrote is in.
			v.done = true
		}
	}
	return changed
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
		name, what = t.Type, t.Desc
		if name == "local_bash" {
			name = "shell"
		}
	}
	if v := s.subs[id]; v != nil && v.log != nil {
		if v.log.Meta.Type != "" {
			name = v.log.Meta.Type
		}
		if what == "" {
			what = v.log.Meta.Desc
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
