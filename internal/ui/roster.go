package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// While an agent has work in flight, what is actually happening is several
// things at once: the agent itself, and each subagent it handed something to.
// A count in the sidebar says how many; it does not say what they are doing,
// and a picker you have to open says nothing until you open it. So the
// running work gets a few rows of its own under the status bar, the way the
// CLI's own interface shows them: who, what they are on now, how long they
// have been at it, and what they have written.
//
// The rows are only there while something is running. An idle agent gets its
// screen back.

// maxRosterRows is the most the block may be, main row included. The CLI
// has two ways of showing background work and so does crema: up to this many
// running, each gets a row of its own under the status bar and ↓ walks onto
// them; past it the rows would be eating the conversation, so the hub shows
// the counts alone — "6 agents · 2 shells" — and the walk stops on that,
// where enter opens the list.
const maxRosterRows = 6

// taskLinger is how long a subagent that ended badly stays on the roster
// after it stopped. The CLI's own panel does this: a subagent that succeeded
// disappears at once, because its answer has already arrived in the
// conversation, while one that failed or was stopped is held for half a
// minute — otherwise the row vanishes at the same instant as the reason for
// it, and you are left with a screen that never mentioned the failure.
const taskLinger = 30 * time.Second

// endedWell reports whether a task's ending needs no further explanation. A
// completed subagent has already said what it found; the others have not.
func endedWell(status string) bool {
	switch status {
	case "completed", "finished", "success":
		return true
	}
	return false
}

// rosterRow is one line: a worker, what it is doing, and its numbers.
type rosterRow struct {
	main bool
	task string // the CLI's id, "" for the main row
	name string
	what string
	dur  time.Duration
	out  int64
	// depth is how far down the subagent tree this row is: 1 for one the
	// agent launched itself, 2 for one of its subagents' subagents. The rows
	// are indented by it, the way the CLI draws the tree.
	depth int
	// gone is set on a row being kept for its last few seconds after ending
	// badly, which is drawn differently — it is a record, not work.
	gone bool
}

// rosterPick is which row the walk is on, for the highlight.
type rosterPick struct {
	main bool
	task string
}

// rosterShown reports which of the two cases this agent is in: rows for
// each running task, or the counts alone.
func (s *Session) rosterShown() bool {
	n := len(s.roster())
	return n > 0 && n <= maxRosterRows
}

// roster is the agent and its running background work, main first. Empty when
// nothing is in flight, which is what keeps the block off the screen.
func (s *Session) roster() []rosterRow {
	var running []rosterRow
	for _, t := range s.tasks {
		gone := false
		switch {
		case t.Status == "running":
		case endedWell(t.Status):
			continue // it answered; the answer is in the conversation
		case t.Ended.IsZero() || time.Since(t.Ended) > taskLinger:
			continue // long enough ago that the row is no longer news
		default:
			gone = true
		}
		name := t.Type
		if name == "local_bash" {
			name = "shell"
		}
		what := descLine(t.Desc)
		if t.LastTool != "" {
			what = t.LastTool + ": " + what
		}
		if gone {
			what = t.Status
			if t.Desc != "" {
				what = t.Status + " · " + descLine(t.Desc)
			}
		}
		var d time.Duration
		if !t.Started.IsZero() {
			d = time.Since(t.Started)
			if gone {
				d = t.Ended.Sub(t.Started) // what it took, not what has passed since
			}
		}
		running = append(running, rosterRow{
			task: t.ID, name: name, what: what, dur: d, out: t.Tokens,
			depth: t.SpawnDepth, gone: gone,
		})
	}
	if len(running) == 0 {
		return nil // nothing to show, and nothing worth the rows
	}
	main := rosterRow{main: true, name: "main", what: s.activity, dur: s.Elapsed(), out: s.turnOut}
	if !s.busy {
		main.what, main.dur = "waiting on the background work", 0
	}
	return append([]rosterRow{main}, running...)
}

// RosterHeight is how many rows the block takes: none when nothing is
// running, and none when so much is that the hub shows counts instead. The
// frame subtracts it from the panes, like everything else at the bottom.
func (s *Session) RosterHeight() int {
	if !s.rosterShown() {
		return 0
	}
	return len(s.roster())
}

// descLine is a task's description as one row. A shell's description is its
// command, and a command can be a whole script — a heredoc writing a file,
// a loop — so drawn as it came, every line of it became a row under the
// status bar and pushed the rest of the screen out of the way. The first line
// says what it is; a trailing … says there is more.
func descLine(d string) string {
	d = strings.TrimSpace(d)
	first, rest, more := strings.Cut(d, "\n")
	first = strings.Join(strings.Fields(first), " ")
	if more && strings.TrimSpace(rest) != "" {
		first += " …"
	}
	return first
}

// rosterName is what goes in the name column, indented by how far down the
// tree the row is. A subagent of a subagent reads as one, rather than as
// another peer of the agent that launched its launcher.
func rosterName(r rosterRow) string {
	if r.depth > 1 {
		return strings.Repeat("  ", min(r.depth-1, 2)) + r.name
	}
	return r.name
}

// rosterView draws the block. Column widths come from the pane rather than
// from the content, so the rows line up with each other and stay put as the
// text under them changes.
func (s *Session) rosterView(w int, pick rosterPick) string {
	if !s.rosterShown() || w < 20 {
		return ""
	}
	rows := s.roster()
	nameW := min(max(w/5, 12), 28)
	var out []string
	for _, r := range rows {
		mark, nameC := "○ ", T.Lilac
		if r.main {
			mark, nameC = "● ", T.Fg
		}
		if r.gone {
			mark, nameC = "⨯ ", T.Muted
		}
		if (r.main && pick.main) || (r.task != "" && r.task == pick.task) {
			// The walk is on this row: the same inversion the hub's own
			// values get, so one look says what enter would act on.
			out = append(out, rosterLine(w, nameW, r, true))
			continue
		}
		nums := ""
		if r.dur > 0 {
			nums = shortDur(r.dur)
		}
		if r.out > 0 {
			if nums != "" {
				nums += " · "
			}
			nums += "↓ " + shortCount(r.out)
		}
		// The row is: a space, the two-column marker, the name, a space, what
		// it is doing, a space, the numbers. What it is doing takes whatever
		// is left, so the numbers stay on the row rather than wrapping onto
		// the next one and pushing the frame out of shape.
		numW := lipgloss.Width(nums)
		whatW := w - 3 - nameW - 1 - numW - 1
		line := base().Foreground(T.Muted).Render(" " + mark)
		line += base().Foreground(nameC).Bold(r.main).Render(pad(clip(rosterName(r), nameW), nameW))
		if whatW > 4 {
			line += base().Render(" ") + base().Foreground(T.Muted).Render(pad(clip(r.what, whatW), whatW))
		}
		if numW > 0 {
			line += base().Render(" ") + base().Foreground(T.Purple).Render(nums)
		}
		out = append(out, base().Width(max(1, w)).Render(line))
	}
	return strings.Join(out, "\n")
}

// rosterLine is one row painted as the selected one: inverted end to end,
// the numbers included, so the highlight reads as a bar and not a smudge.
func rosterLine(w, nameW int, r rosterRow, _ bool) string {
	nums := ""
	if r.dur > 0 {
		nums = shortDur(r.dur)
	}
	if r.out > 0 {
		if nums != "" {
			nums += " · "
		}
		nums += "↓ " + shortCount(r.out)
	}
	mark := "○ "
	switch {
	case r.main:
		mark = "● "
	case r.gone:
		mark = "⨯ "
	}
	numW := lipgloss.Width(nums)
	whatW := w - 3 - nameW - 1 - numW - 1
	text := " " + mark + pad(clip(rosterName(r), nameW), nameW)
	if whatW > 4 {
		// Whatever put the text here, a row is one row: the main agent's own
		// activity can be a multi-line command just as a shell's can.
		text += " " + pad(clip(descLine(r.what), whatW), whatW)
	}
	if numW > 0 {
		text += " " + nums
	}
	return lipgloss.NewStyle().Background(T.Purple).Foreground(T.Surface).Bold(true).
		Width(max(1, w)).Render(text)
}

// pad right-fills to width so the columns after it start in the same place on
// every row.
func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// shortDur is how long something has been going, in the units a person would
// say it in: 42s, then 6m 51s, then 2h 04m.
func shortDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}
