package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	statusHeight = 1
	diffMinCols  = 100 // narrower than this, the diff pane hides so the timeline stays readable
	diffDebounce = 350 * time.Millisecond
)

type Layout struct {
	TimelineW, DiffW, PaneH int
	ShowDiff                bool
}

func ComputeLayout(w, h int, wantDiff bool) Layout {
	paneH := h - InputHeight - statusHeight
	if paneH < 3 {
		paneH = 3
	}
	l := Layout{PaneH: paneH, TimelineW: max(1, w)}
	if wantDiff && w >= diffMinCols {
		dw := w * 42 / 100
		if dw > 70 {
			dw = 70
		}
		if dw < 34 {
			dw = 34
		}
		l.ShowDiff, l.DiffW, l.TimelineW = true, dw, w-dw
	}
	return l
}

type agentEventMsg struct {
	id int
	ev agent.Event
}

type streamStartedMsg struct {
	id  int
	ch  <-chan agent.Event
	err error
}

type streamClosedMsg struct{ id int }

type diffMsg struct {
	seq int
	ds  gitdiff.DiffSet
}

type diffTickMsg struct{ seq int }

type focusTarget int

const (
	focusInput focusTarget = iota
	focusTimeline
	focusDiff
)

type App struct {
	reg *agent.Registry
	cur agent.Agent
	dir string

	tl *Timeline
	dp *DiffPanel
	in *Input
	sp spinner.Model

	w, h     int
	lay      Layout
	wantDiff bool
	focus    focusTarget

	busy      bool
	turnStart time.Time
	stream    <-chan agent.Event
	streamID  int
	cancel    context.CancelFunc
	lastOpts  agent.RunOptions // last Run options, for tests and the status line

	sessions    map[string]string
	sessionCost float64
	diff        gitdiff.DiffSet
	diffSeq     int
	diffApplied int
	note        string
}

func NewApp(reg *agent.Registry, cur agent.Agent, dir string) *App {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(T.Pink)
	a := &App{
		reg: reg, cur: cur, dir: dir,
		tl: NewTimeline(80, 20), dp: NewDiffPanel(40, 20), in: NewInput(80),
		sp: sp, wantDiff: true, sessions: map[string]string{},
	}
	a.tl.Append(Block{Kind: BlockSystem, Text: fmt.Sprintf(
		"crema · %s · %s · every command and edit is shown in full",
		cur.Label(), permissionNote(cur))})
	// Render a real frame from the start. Terminals send a WindowSizeMsg
	// immediately, but a pipe or an odd SSH client may not, and a bare
	// placeholder would be all the user ever sees.
	a.resize(80, 24)
	return a
}

func permissionNote(a agent.Agent) string {
	switch a.Name() {
	case "claude":
		return "permission mode acceptEdits — file edits apply without asking"
	case "codex":
		return "sandbox full-auto — file edits apply without asking"
	default:
		return "demo agent, no real work performed"
	}
}

func modeLabel(a agent.Agent) string {
	switch a.Name() {
	case "claude":
		return "acceptEdits"
	case "codex":
		return "full-auto"
	default:
		return "demo"
	}
}

func (a *App) Init() tea.Cmd { return tea.Batch(a.in.Focus(), a.collectDiff()) }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)

	case tea.MouseMsg:
		if a.lay.ShowDiff && msg.X >= a.lay.TimelineW {
			return a, a.dp.Update(msg)
		}
		return a, a.tl.Update(msg)

	case spinner.TickMsg:
		if !a.busy {
			return a, nil
		}
		var cmd tea.Cmd
		a.sp, cmd = a.sp.Update(msg)
		return a, cmd

	case streamStartedMsg:
		if msg.id != a.streamID {
			if msg.ch != nil {
				go drain(msg.ch) // superseded stream: let its goroutine finish
			}
			return a, nil
		}
		if msg.err != nil {
			a.tl.Append(Block{Kind: BlockError, Text: msg.err.Error()})
			a.endTurn(nil)
			return a, nil
		}
		a.stream = msg.ch
		return a, waitForEvent(msg.ch, msg.id)

	case agentEventMsg:
		if msg.id != a.streamID {
			return a, nil
		}
		a.tl.AppendEvent(msg.ev)
		cmds := []tea.Cmd{waitForEvent(a.stream, msg.id)}
		switch msg.ev.Kind {
		case agent.KindToolOutput:
			cmds = append(cmds, a.scheduleDiff())
		case agent.KindTurnEnd:
			a.endTurn(msg.ev.Result)
			cmds = append(cmds, a.collectDiff())
		}
		return a, tea.Batch(cmds...)

	case streamClosedMsg:
		if msg.id != a.streamID {
			return a, nil
		}
		if a.busy { // adapters guarantee a TurnEnd; this is the belt-and-braces path
			a.tl.Append(Block{Kind: BlockError, Text: "the agent stream ended without finishing the turn"})
			a.endTurn(nil)
		}
		return a, nil

	case diffTickMsg:
		if msg.seq != a.diffSeq {
			return a, nil // a newer edit landed; that tick will do the work
		}
		return a, a.collectDiff()

	case diffMsg:
		if msg.seq < a.diffApplied {
			return a, nil // out-of-order result
		}
		a.diffApplied = msg.seq
		a.diff = msg.ds
		a.dp.SetDiff(msg.ds)
		return a, nil
	}
	return a, a.routeToFocus(msg)
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if a.cancel != nil {
			a.cancel()
		}
		return a, tea.Quit
	case "esc":
		if a.busy && a.cancel != nil {
			a.cancel()
			a.tl.Append(Block{Kind: BlockSystem, Text: "canceling the current turn…"})
		}
		return a, nil
	case "tab":
		return a, a.switchAgent()
	case "ctrl+t":
		a.wantDiff = !a.wantDiff
		a.resize(a.w, a.h)
		return a, nil
	case "ctrl+r":
		return a, a.collectDiff()
	case "ctrl+o":
		return a, a.cycleFocus()
	case "enter":
		if a.focus != focusInput {
			return a, nil
		}
		text := strings.TrimSpace(a.in.Value())
		if text == "" {
			return a, nil
		}
		if a.busy {
			a.note = "turn in progress — esc to cancel"
			return a, nil
		}
		a.in.Reset()
		return a, a.startTurn(text)
	}
	return a, a.routeToFocus(msg)
}

func (a *App) startTurn(prompt string) tea.Cmd {
	if err := a.cur.Available(); err != nil {
		a.tl.Append(Block{Kind: BlockError, Text: err.Error()})
		return nil
	}
	a.tl.Append(Block{Kind: BlockUser, Text: prompt})
	a.streamID++
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.busy = true
	a.note = ""
	a.turnStart = time.Now()
	a.lastOpts = agent.RunOptions{Prompt: prompt, Dir: a.dir, SessionID: a.sessions[a.cur.Name()]}
	return tea.Batch(startStream(a.cur, ctx, a.lastOpts, a.streamID), a.sp.Tick)
}

func (a *App) endTurn(r *agent.TurnResult) {
	a.busy = false
	a.stream = nil
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if r != nil {
		if r.SessionID != "" {
			a.sessions[a.cur.Name()] = r.SessionID
		}
		a.sessionCost += r.CostUSD
	}
}

func (a *App) switchAgent() tea.Cmd {
	if a.busy {
		a.note = "finish or cancel the current turn before switching"
		return nil
	}
	next := a.reg.Next(a.cur.Name())
	if next == nil || next == a.cur {
		a.tl.Append(Block{Kind: BlockSystem, Text: "no other agent is available"})
		return nil
	}
	a.cur = next
	a.note = ""
	a.tl.Append(Block{Kind: BlockSystem, Text: fmt.Sprintf("switched to %s · %s · %s",
		next.Label(), permissionNote(next), resumeNote(a.sessions[next.Name()]))})
	return nil
}

func resumeNote(sid string) string {
	if sid == "" {
		return "new session"
	}
	return "resuming session " + sid
}

func (a *App) cycleFocus() tea.Cmd {
	switch a.focus {
	case focusInput:
		a.focus = focusTimeline
		a.in.Blur()
		return nil
	case focusTimeline:
		if a.lay.ShowDiff {
			a.focus = focusDiff
			return nil
		}
	}
	a.focus = focusInput
	return a.in.Focus()
}

func (a *App) routeToFocus(msg tea.Msg) tea.Cmd {
	switch a.focus {
	case focusTimeline:
		return a.tl.Update(msg)
	case focusDiff:
		return a.dp.Update(msg)
	default:
		return a.in.Update(msg)
	}
}

func (a *App) resize(w, h int) {
	a.w, a.h = w, h
	a.lay = ComputeLayout(w, h, a.wantDiff)
	a.tl.SetSize(a.lay.TimelineW, a.lay.PaneH)
	if a.lay.ShowDiff {
		a.dp.SetSize(a.lay.DiffW-2, a.lay.PaneH-2) // inside the rounded border
	} else if a.focus == focusDiff {
		a.focus = focusInput
		a.in.Focus()
	}
	a.in.SetWidth(w)
}

func (a *App) View() string {
	if a.w == 0 || a.h == 0 {
		return "starting crema…"
	}
	main := a.tl.View()
	if a.lay.ShowDiff {
		c := T.Muted
		if a.focus == focusDiff {
			c = T.Purple
		}
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(c).
			Width(a.lay.DiffW - 2).Height(a.lay.PaneH - 2).
			Render(a.dp.View())
		main = lipgloss.JoinHorizontal(lipgloss.Top, a.tl.View(), box)
	}
	return strings.Join([]string{main, a.in.View(), a.statusLine()}, "\n")
}

func (a *App) statusLine() string {
	s := StatusData{
		Agent: a.cur.Label(), Mode: modeLabel(a.cur), Dir: a.dir,
		Busy: a.busy, Spin: a.sp.View(), Cost: a.sessionCost,
		Adds: a.diff.Additions, Dels: a.diff.Deletions, Note: a.note,
	}
	if a.busy {
		s.ElapsedSec = time.Since(a.turnStart).Seconds()
	}
	return RenderStatus(s, a.w)
}

func startStream(ag agent.Agent, ctx context.Context, opts agent.RunOptions, id int) tea.Cmd {
	return func() tea.Msg {
		ch, err := ag.Run(ctx, opts)
		return streamStartedMsg{id: id, ch: ch, err: err}
	}
}

func waitForEvent(ch <-chan agent.Event, id int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{id: id}
		}
		return agentEventMsg{id: id, ev: ev}
	}
}

func drain(ch <-chan agent.Event) {
	for range ch {
	}
}

func (a *App) collectDiff() tea.Cmd {
	a.diffSeq++
	seq, dir := a.diffSeq, a.dir
	return func() tea.Msg { return diffMsg{seq: seq, ds: gitdiff.Collect(dir)} }
}

// scheduleDiff debounces refreshes: only the newest schedule survives, so a
// burst of edits costs one `git diff` instead of one per file.
func (a *App) scheduleDiff() tea.Cmd {
	a.diffSeq++
	seq := a.diffSeq
	return tea.Tick(diffDebounce, func(time.Time) tea.Msg { return diffTickMsg{seq: seq} })
}
