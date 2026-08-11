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
	// Hard floors: below these the pane is dropped no matter what the user
	// asked for, because the timeline would stop being readable. Above them
	// ctrl+b / ctrl+t decide. 70 keeps a 46-column timeline beside the sidebar.
	sidebarMinCols = 70
	diffMinCols    = 124 // the diff pane needs room left over after the sidebar
	diffDebounce   = 350 * time.Millisecond
)

type Layout struct {
	SidebarW, TimelineW, DiffW, PaneH int
	ShowSidebar, ShowDiff             bool
}

// ComputeLayout splits the terminal into sidebar | timeline | diff, dropping
// the optional panes as the terminal narrows so 80x24 stays usable.
func ComputeLayout(w, h int, wantSidebar, wantDiff bool) Layout {
	paneH := h - InputHeight - statusHeight
	if paneH < 3 {
		paneH = 3
	}
	l := Layout{PaneH: paneH, TimelineW: max(1, w)}
	if wantSidebar && w >= sidebarMinCols {
		l.ShowSidebar = true
		l.SidebarW = SidebarWidth
		l.TimelineW = max(1, w-SidebarWidth)
	}
	if wantDiff && w >= diffMinCols {
		dw := w * 34 / 100
		if dw > 70 {
			dw = 70
		}
		if dw < 34 {
			dw = 34
		}
		l.ShowDiff = true
		l.DiffW = dw
		l.TimelineW = max(1, w-l.SidebarW-dw)
	}
	return l
}

type agentEventMsg struct {
	sess, seq int
	ev        agent.Event
}

type streamStartedMsg struct {
	sess, seq int
	ch        <-chan agent.Event
	err       error
}

type streamClosedMsg struct{ sess, seq int }

type diffMsg struct {
	sess, seq int
	ds        gitdiff.DiffSet
}

type diffTickMsg struct{ sess, seq int }

type focusTarget int

const (
	focusInput focusTarget = iota
	focusTimeline
	focusDiff
)

type App struct {
	reg *agent.Registry

	sessions []*Session
	active   int
	nextID   int

	in     *Input
	sp     spinner.Model
	picker *Picker

	w, h        int
	lay         Layout
	wantSidebar bool
	wantDiff    bool
	focus       focusTarget
	note        string
}

// NewApp opens a single session for cur in dir; more are added with ctrl+n.
func NewApp(reg *agent.Registry, cur agent.Agent, dir string) *App {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(T.Pink)
	a := &App{
		reg: reg, in: NewInput(80), sp: sp,
		wantSidebar: true, wantDiff: true,
	}
	a.addSession(cur, dir)
	// Render a real frame from the start. Terminals send a WindowSizeMsg
	// immediately, but a pipe or an odd SSH client may not, and a bare
	// placeholder would be all the user ever sees.
	a.resize(80, 24)
	return a
}

func (a *App) addSession(backend agent.Agent, dir string) *Session {
	a.nextID++
	s := NewSession(a.nextID, backend, dir)
	a.sessions = append(a.sessions, s)
	a.active = len(a.sessions) - 1
	return s
}

// Sessions exposes the open agents (used by the sidebar and by tests).
func (a *App) Sessions() []*Session { return a.sessions }

func (a *App) cur() *Session {
	if len(a.sessions) == 0 {
		return nil
	}
	if a.active >= len(a.sessions) {
		a.active = len(a.sessions) - 1
	}
	return a.sessions[a.active]
}

func (a *App) sessionByID(id int) *Session {
	for _, s := range a.sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
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

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.in.Focus()}
	for _, s := range a.sessions {
		cmds = append(cmds, s.collectDiff())
	}
	return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return a, nil

	case tea.KeyMsg:
		if a.picker != nil {
			return a, a.updatePicker(msg)
		}
		return a.handleKey(msg)

	case tea.MouseMsg:
		return a, a.routeMouse(msg)

	case spinner.TickMsg:
		if !a.anyBusy() {
			return a, nil
		}
		var cmd tea.Cmd
		a.sp, cmd = a.sp.Update(msg)
		return a, cmd

	case streamStartedMsg:
		s := a.sessionByID(msg.sess)
		if s == nil || msg.seq != s.streamSeq {
			if msg.ch != nil {
				go drain(msg.ch) // superseded or closed session: let its goroutine finish
			}
			return a, nil
		}
		if msg.err != nil {
			s.tl.Append(Block{Kind: BlockError, Text: msg.err.Error()})
			s.endTurn(nil)
			return a, nil
		}
		s.stream = msg.ch
		return a, waitForEvent(msg.ch, s.ID, msg.seq)

	case agentEventMsg:
		s := a.sessionByID(msg.sess)
		if s == nil || msg.seq != s.streamSeq {
			return a, nil
		}
		s.tl.AppendEvent(msg.ev)
		cmds := []tea.Cmd{waitForEvent(s.stream, s.ID, msg.seq)}
		switch msg.ev.Kind {
		case agent.KindToolOutput:
			cmds = append(cmds, s.scheduleDiff())
		case agent.KindTurnEnd:
			s.endTurn(msg.ev.Result)
			cmds = append(cmds, s.collectDiff())
		}
		return a, tea.Batch(cmds...)

	case streamClosedMsg:
		s := a.sessionByID(msg.sess)
		if s == nil || msg.seq != s.streamSeq {
			return a, nil
		}
		if s.busy { // adapters guarantee a TurnEnd; this is the belt-and-braces path
			s.tl.Append(Block{Kind: BlockError, Text: "the agent stream ended without finishing the turn"})
			s.endTurn(nil)
		}
		return a, nil

	case diffTickMsg:
		s := a.sessionByID(msg.sess)
		if s == nil || msg.seq != s.diffSeq {
			return a, nil // a newer edit landed; that tick will do the work
		}
		return a, s.collectDiff()

	case diffMsg:
		s := a.sessionByID(msg.sess)
		if s == nil || msg.seq < s.diffApplied {
			return a, nil // out-of-order result
		}
		s.diffApplied = msg.seq
		s.diff = msg.ds
		s.dp.SetDiff(msg.ds)
		return a, nil
	}
	return a, a.routeToFocus(msg)
}

func (a *App) anyBusy() bool {
	for _, s := range a.sessions {
		if s.busy {
			return true
		}
	}
	return false
}

func (a *App) updatePicker(msg tea.KeyMsg) tea.Cmd {
	done, canceled := a.picker.Update(msg)
	switch {
	case canceled:
		a.picker = nil
		return nil
	case done:
		backend, dir := a.picker.Result()
		a.picker = nil
		s := a.addSession(backend, dir)
		a.resize(a.w, a.h)
		a.note = ""
		return tea.Batch(s.collectDiff(), a.in.Focus())
	}
	return nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		for _, s := range a.sessions {
			s.close()
		}
		return a, tea.Quit
	case "ctrl+n":
		start := "."
		if s := a.cur(); s != nil {
			start = s.Dir
		}
		a.picker = NewPicker(a.reg.Agents, start)
		return a, nil
	case "ctrl+w":
		return a, a.closeSession()
	case "esc":
		if s := a.cur(); s != nil {
			s.cancelTurn()
		}
		return a, nil
	case "tab":
		a.selectSession(a.active + 1)
		return a, nil
	case "shift+tab":
		a.selectSession(a.active - 1)
		return a, nil
	case "ctrl+b":
		a.wantSidebar = !a.wantSidebar
		a.resize(a.w, a.h)
		return a, nil
	case "ctrl+t":
		a.wantDiff = !a.wantDiff
		a.resize(a.w, a.h)
		return a, nil
	case "ctrl+l":
		a.note = "theme: " + ToggleMode().String()
		a.applyTheme()
		return a, nil
	case "ctrl+r":
		if s := a.cur(); s != nil {
			return a, s.collectDiff()
		}
		return a, nil
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
		s := a.cur()
		if s == nil {
			return a, nil
		}
		if s.busy {
			a.note = "this agent is busy — esc to cancel, or ctrl+n for another"
			return a, nil
		}
		a.in.Reset()
		a.note = ""
		return a, tea.Batch(s.startTurn(text), a.sp.Tick)
	}
	// alt+1..9 jumps straight to a session
	if k := msg.String(); strings.HasPrefix(k, "alt+") && len(k) == 5 {
		if d := k[4]; d >= '1' && d <= '9' {
			a.selectSession(int(d - '1'))
			return a, nil
		}
	}
	return a, a.routeToFocus(msg)
}

// applyTheme re-renders everything that caches styled text.
func (a *App) applyTheme() {
	a.sp.Style = lipgloss.NewStyle().Foreground(T.Pink)
	a.in.ApplyTheme()
	for _, s := range a.sessions {
		s.tl.Invalidate()
		s.dp.Invalidate()
	}
}

func (a *App) selectSession(i int) {
	n := len(a.sessions)
	if n == 0 {
		return
	}
	a.active = ((i % n) + n) % n
	a.note = ""
	a.resize(a.w, a.h)
}

// closeSession ends the focused agent. Closing the last one quits.
func (a *App) closeSession() tea.Cmd {
	s := a.cur()
	if s == nil {
		return tea.Quit
	}
	s.close()
	a.sessions = append(a.sessions[:a.active], a.sessions[a.active+1:]...)
	if len(a.sessions) == 0 {
		return tea.Quit
	}
	if a.active >= len(a.sessions) {
		a.active = len(a.sessions) - 1
	}
	a.resize(a.w, a.h)
	return nil
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

func (a *App) routeMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.cur()
	if s == nil {
		return nil
	}
	if a.lay.ShowDiff && msg.X >= a.lay.SidebarW+a.lay.TimelineW {
		return s.dp.Update(msg)
	}
	if msg.X >= a.lay.SidebarW {
		return s.tl.Update(msg)
	}
	return nil
}

func (a *App) routeToFocus(msg tea.Msg) tea.Cmd {
	s := a.cur()
	if s == nil {
		return nil
	}
	switch a.focus {
	case focusTimeline:
		return s.tl.Update(msg)
	case focusDiff:
		return s.dp.Update(msg)
	default:
		return a.in.Update(msg)
	}
}

func (a *App) resize(w, h int) {
	a.w, a.h = w, h
	a.lay = ComputeLayout(w, h, a.wantSidebar, a.wantDiff)
	for _, s := range a.sessions {
		s.SetSize(a.lay.TimelineW, a.lay.DiffW, a.lay.PaneH)
	}
	if !a.lay.ShowDiff && a.focus == focusDiff {
		a.focus = focusInput
		a.in.Focus()
	}
	a.in.SetWidth(w)
}

func (a *App) View() string {
	if a.w == 0 || a.h == 0 {
		return "starting crema…"
	}
	if a.picker != nil {
		modal := a.picker.View(min(a.w, 72), a.lay.PaneH+InputHeight)
		modal = lipgloss.PlaceHorizontal(a.w, lipgloss.Center, modal)
		return modal + "\n" + a.statusLine()
	}

	s := a.cur()
	var panes []string
	if a.lay.ShowSidebar {
		panes = append(panes, lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(T.Muted).
			Width(a.lay.SidebarW-2).Height(a.lay.PaneH-2).
			Render(RenderSidebar(a.sessions, a.active, a.sp.View(), a.lay.SidebarW-2, a.lay.PaneH-2)))
	}
	if s != nil {
		panes = append(panes, s.tl.View())
		if a.lay.ShowDiff {
			c := T.Muted
			if a.focus == focusDiff {
				c = T.Purple
			}
			panes = append(panes, lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).BorderForeground(c).
				Width(a.lay.DiffW-2).Height(a.lay.PaneH-2).
				Render(s.dp.View()))
		}
	}
	main := lipgloss.JoinHorizontal(lipgloss.Top, panes...)
	return strings.Join([]string{main, a.in.View(), a.statusLine()}, "\n")
}

func (a *App) statusLine() string {
	s := a.cur()
	if s == nil {
		return RenderStatus(StatusData{Agent: "no agents", Mode: "—", Note: a.note}, a.w)
	}
	d := StatusData{
		Agent: s.Backend.Label(), Mode: modeLabel(s.Backend), Dir: s.Dir,
		Busy: s.busy, Spin: a.sp.View(), Cost: s.cost,
		Adds: s.diff.Additions, Dels: s.diff.Deletions, Note: a.note,
	}
	if n := len(a.sessions); n > 1 {
		running := 0
		for _, x := range a.sessions {
			if x.busy {
				running++
			}
		}
		d.Agent = fmt.Sprintf("%s [%d/%d]", d.Agent, a.active+1, n)
		if running > 0 && a.note == "" {
			d.Note = fmt.Sprintf("%d running", running)
		}
	}
	if s.busy {
		d.ElapsedSec = s.Elapsed().Seconds()
	}
	return RenderStatus(d, a.w)
}

func startStream(ag agent.Agent, ctx context.Context, opts agent.RunOptions, sess, seq int) tea.Cmd {
	return func() tea.Msg {
		ch, err := ag.Run(ctx, opts)
		return streamStartedMsg{sess: sess, seq: seq, ch: ch, err: err}
	}
}

func waitForEvent(ch <-chan agent.Event, sess, seq int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{sess: sess, seq: seq}
		}
		return agentEventMsg{sess: sess, seq: seq, ev: ev}
	}
}

func drain(ch <-chan agent.Event) {
	for range ch {
	}
}
