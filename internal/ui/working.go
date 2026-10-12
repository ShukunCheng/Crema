package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/crlog"
	"github.com/charmbracelet/lipgloss"
)

// While a turn runs there is nothing to read yet and everything to wonder
// about: is it alive, how long has it been, what is it doing. The status bar
// answers the first two in the corner of the screen. This answers all three
// where you are already looking — the bottom of the conversation, under the
// last thing the agent said, the way Claude Code's own spinner line does.
//
// Every part of it is measured rather than decorated: the verb is the last
// event that arrived, the clock is this turn's, and the token count is what
// the backend reported writing so far.
func (s *Session) workingLine(spin string, w int) string {
	if !s.busy {
		return ""
	}
	parts := []string{fmt.Sprintf("%.0fs", s.Elapsed().Seconds())}
	if s.turnOut > 0 {
		parts = append(parts, "↓ "+shortCount(s.turnOut)+" tokens")
	}
	if n := s.RunningTasks(); n > 0 {
		word := "task"
		if n > 1 {
			word = "tasks"
		}
		parts = append(parts, fmt.Sprintf("%d background %s", n, word))
	}
	parts = append(parts, "esc to cancel")

	line := strings.TrimSpace(spin) + " " + s.activity + "… (" + strings.Join(parts, " · ") + ")"
	return lipgloss.NewStyle().Background(T.Bg).Foreground(T.Muted).
		Width(max(1, w)).Render(clip(line, max(1, w))) + "\n"
}

// shortCount writes a token count the way a person would say it: 812, 1.2k,
// 34k. The exact figure is on the turn's own footer when it finishes.
func shortCount(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%dk", n/1000)
}

// noteActivity keeps the working line honest about what is happening now. The
// backends do not announce what they are up to, so the last event is the
// evidence: a tool call means that tool is running, and prose means it is
// writing the answer.
// resumeTurnIfWorking marks the agent busy again when its own main agent starts
// working without a message having been sent — the CLI continuing a turn once
// the background work it was waiting on is done. It reports whether it did.
//
// Found in crema's own log: one agent finished nine subagents over eighteen
// minutes, launching and collecting them, with no turn start and no turn end
// anywhere in that window. It was working the whole time, and whenever no
// subagent happened to be running at that instant, the sidebar called it idle.
//
// Only the main agent's own work counts. A subagent's streamed events carry the
// id of the tool call that launched it, and a subagent working is already what
// the running count is for — it is not the main agent picking back up.
func (s *Session) resumeTurnIfWorking(ev agent.Event) bool {
	if s.busy || ev.SubID != "" {
		return false
	}
	switch ev.Kind {
	case agent.KindText, agent.KindToolCall, agent.KindToolOutput:
	default:
		return false
	}
	s.busy = true
	s.turnStart = time.Now()
	s.activity, s.turnOut, s.turnEvents = "continuing", 0, 0
	s.canceled = false
	crlog.Log("turn.resume", "agent", s.ID, "running_tasks", s.RunningTasks(),
		"first", kindName(ev))
	return true
}

// kindName is an event kind as the log wants to read it.
func kindName(ev agent.Event) string {
	switch ev.Kind {
	case agent.KindText:
		if ev.Thinking {
			return "thinking"
		}
		return "text"
	case agent.KindToolCall:
		return "tool_call"
	case agent.KindToolOutput:
		return "tool_output"
	}
	return "other"
}

func (s *Session) noteActivity(ev agent.Event) {
	if ev.Kind != agent.KindTurnEnd && ev.Kind != agent.KindControl {
		// Anything the backend sent mid-turn counts as the turn having said
		// something — the silent-turn notice keys off this staying at zero.
		// A control reply is crema being answered, not the turn speaking.
		s.turnEvents++
	}
	act := ""
	switch ev.Kind {
	case agent.KindText:
		if ev.Thinking {
			act = "thinking"
		} else {
			act = "writing"
		}
	case agent.KindToolCall:
		if ev.Tool != nil && ev.Tool.Name != "" {
			act = ev.Tool.Name
		}
	case agent.KindToolOutput:
		act = "reading the result"
	case agent.KindTask:
		// A subagent's heartbeat names what it is running right now. A
		// backgrounded command is not the turn's work, so it only counts in
		// the background tally, not here.
		if t := ev.Task; t != nil && t.Status == "running" && t.Type != "local_bash" && t.LastTool != "" {
			act = "subagent: " + t.LastTool
		}
	}
	if act != "" && ev.SubID != "" {
		act = "subagent: " + act
	}
	if act != "" {
		s.activity = act
	}
	if ev.OutTokens > s.turnOut {
		s.turnOut = ev.OutTokens
	}
}
