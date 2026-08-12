package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/atotto/clipboard"
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

	in       *Input
	sp       spinner.Model
	picker   *Picker
	settings *Settings

	w, h        int
	lay         Layout
	wantSidebar bool
	wantDiff    bool
	dragging    bool // a text selection is being dragged in the timeline
	focus       focusTarget
	note        string
}

// NewApp opens a single session for cur in dir; more are added with ctrl+n.
func NewApp(reg *agent.Registry, cur agent.Agent, dir string) *App {
	a := newBareApp(reg)
	a.addSession(cur, dir).introduce()
	a.resize(80, 24)
	return a
}

// NewAppRestored rebuilds the agents saved by a previous run, falling back to a
// single session for cur in dir when there is nothing to restore.
func NewAppRestored(reg *agent.Registry, st State, cur agent.Agent, dir string) *App {
	a := newBareApp(reg)
	n, skipped := a.RestoreSessions(st)
	if n == 0 {
		a.addSession(cur, dir).introduce()
	}
	for _, s := range skipped {
		a.cur().tl.Append(Block{Kind: BlockError, Text: "could not restore " + s})
	}
	a.resize(80, 24)
	return a
}

func newBareApp(reg *agent.Registry) *App {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = fg(T.Pink)
	return &App{
		reg: reg, in: NewInput(80), sp: sp,
		wantSidebar: true, wantDiff: true,
	}
	// Callers resize immediately: terminals send a WindowSizeMsg on startup,
	// but a pipe or an odd SSH client may not, and a bare placeholder would
	// be all the user ever sees.
}

// ApplyToFocused sets startup flags on the focused agent, overriding whatever
// was restored. Only the flags the user actually typed are applied.
func (a *App) ApplyToFocused(setPerm bool, perm agent.PermissionMode, setModel bool, model string) {
	s := a.cur()
	if s == nil {
		return
	}
	if setPerm {
		s.SetPermission(perm)
	}
	if setModel {
		s.SetModel(model)
	}
}

// EnsureSession focuses an existing agent for backend+dir, or opens one. Used
// when the user names --agent/--dir explicitly on a run that also restores.
func (a *App) EnsureSession(backend agent.Agent, dir string) {
	for i, s := range a.sessions {
		if s.Backend.Name() == backend.Name() && s.Dir == dir {
			a.active = i
			return
		}
	}
	a.addSession(backend, dir).introduce()
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
		if a.settings != nil {
			return a, a.updateSettings(msg)
		}
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
			a.persist() // a finished turn is worth surviving a crash
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

// updateSettings applies a chosen setting to the focused agent. The modal
// stays open so several settings can be changed in one visit.
func (a *App) updateSettings(msg tea.KeyMsg) tea.Cmd {
	chosen, canceled := a.settings.Update(msg)
	return a.applySetting(chosen, canceled)
}

func (a *App) applySetting(chosen *settingsRow, canceled bool) tea.Cmd {
	if canceled {
		a.settings = nil
		return a.in.Focus()
	}
	s := a.cur()
	if chosen == nil || s == nil {
		return nil
	}
	switch chosen.kind {
	case settingsPermission:
		s.SetPermission(chosen.perm)
	case settingsModel:
		s.SetModel(chosen.model)
	}
	a.persist()
	return nil
}

func (a *App) openSettings() {
	if s := a.cur(); s != nil {
		a.settings = NewSettings(s)
	}
}

func (a *App) openPicker() {
	start := "."
	if s := a.cur(); s != nil {
		start = s.Dir
	}
	a.picker = NewPicker(a.reg.Agents, start)
}

func (a *App) updatePicker(msg tea.KeyMsg) tea.Cmd {
	return a.finishPicker(a.picker.Update(msg))
}

// finishPicker applies the picker's verdict, whether it came from a key or a click.
func (a *App) finishPicker(done, canceled bool) tea.Cmd {
	switch {
	case canceled:
		a.picker = nil
		return nil
	case done:
		backend, dir := a.picker.Result()
		a.picker = nil
		s := a.addSession(backend, dir).introduce()
		a.resize(a.w, a.h)
		a.note = ""
		a.persist()
		return tea.Batch(s.collectDiff(), a.in.Focus())
	}
	return nil
}

// modalView renders whichever modal is open, or "" when none is.
func (a *App) modalView() string {
	_, _, w, h := a.modalRect()
	switch {
	case a.settings != nil:
		return a.settings.View(w, h)
	case a.picker != nil:
		return a.picker.View(w, h)
	}
	return ""
}

// modalRect is the open modal's on-screen box, computed identically by View
// and by the click handler.
func (a *App) modalRect() (x, y, w, h int) {
	w = min(a.w, 72)
	h = a.lay.PaneH + InputHeight
	return (a.w - w) / 2, 0, w, h
}

func isLeftClick(m tea.MouseMsg) bool {
	// Wheel events are also "press", so the button has to be checked too.
	return m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		a.persist()
		for _, s := range a.sessions {
			s.close()
		}
		return a, tea.Quit
	case "ctrl+n":
		a.openPicker()
		return a, nil
	case "ctrl+p":
		a.openSettings()
		return a, nil
	case "down":
		// From the input, down opens the model / permission picker — the panel
		// navigates with the same key, so the gesture just keeps going. A
		// multi-line draft keeps down as cursor movement.
		if a.focus == focusInput && !strings.Contains(a.in.Value(), "\n") {
			a.openSettings()
			return a, nil
		}
	case "ctrl+w":
		return a, a.closeSession()
	case "esc":
		if s := a.cur(); s != nil {
			if s.tl.HasSelection() {
				s.tl.ClearSelection() // drop the highlight before touching the turn
				return a, nil
			}
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
		ToggleMode() // the status-bar chip shows the result, so no note needed
		a.applyTheme()
		a.persist()
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
	SyncTerminalBackground() // so the emulator's padding follows too
	a.sp.Style = fg(T.Pink)
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
		a.persist()
		return tea.Quit
	}
	if a.active >= len(a.sessions) {
		a.active = len(a.sessions) - 1
	}
	a.resize(a.w, a.h)
	a.persist()
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
	if a.settings != nil || a.picker != nil {
		if !isLeftClick(msg) {
			return nil
		}
		x, y, w, _ := a.modalRect()
		if msg.X < x+2 || msg.X >= x+w-2 || msg.Y < y+1 {
			return nil // border, padding, or outside the modal
		}
		row := msg.Y - (y + 1)
		if a.settings != nil {
			return a.applySetting(a.settings.ClickRow(row))
		}
		return a.finishPicker(a.picker.ClickRow(row))
	}
	// A plain drag inside the timeline selects text. Mouse reporting is
	// all-or-nothing for the terminal, so selection in one pane has to be
	// drawn by the app; everywhere else keeps behaving like buttons.
	if cmd, handled := a.routeDrag(msg); handled {
		return cmd
	}
	if isLeftClick(msg) {
		return a.handleClick(msg)
	}
	s := a.cur()
	if s == nil {
		return nil
	}
	// Wheel scrolls whichever pane is under the cursor, focus or not.
	if a.lay.ShowDiff && msg.X >= a.lay.SidebarW+a.lay.TimelineW {
		return s.dp.Update(msg)
	}
	if msg.X >= a.lay.SidebarW {
		return s.tl.Update(msg)
	}
	return nil
}

// inTimeline reports whether a screen position is inside the conversation pane.
func (a *App) inTimeline(x, y int) bool {
	return y >= 0 && y < a.lay.PaneH &&
		x >= a.lay.SidebarW && x < a.lay.SidebarW+a.lay.TimelineW
}

// timelinePoint converts screen coordinates to a position in the conversation.
func (a *App) timelinePoint(s *Session, x, y int) (line, col int) {
	return s.tl.YOffset() + y, x - a.lay.SidebarW
}

// routeDrag implements press/drag/release selection in the timeline. It
// reports handled=true only for events it consumed, so a plain click still
// falls through to the button behaviour.
func (a *App) routeDrag(msg tea.MouseMsg) (tea.Cmd, bool) {
	s := a.cur()
	if s == nil || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}
	switch msg.Action {
	case tea.MouseActionPress:
		if !a.inTimeline(msg.X, msg.Y) {
			s.tl.ClearSelection() // clicking elsewhere drops the highlight
			return nil, false
		}
		line, col := a.timelinePoint(s, msg.X, msg.Y)
		s.tl.BeginSelect(line, col)
		a.dragging = true
		// Consume the press. Acting on it now would fold a block out from
		// under a drag that was only just beginning; the timeline's click
		// behaviour runs on release instead, once we know it wasn't a drag.
		return nil, true

	case tea.MouseActionMotion:
		if !a.dragging {
			return nil, false
		}
		s.tl.ExtendSelect(a.timelinePoint(s, msg.X, min(max(msg.Y, 0), a.lay.PaneH-1)))
		return nil, true

	case tea.MouseActionRelease:
		if !a.dragging {
			return nil, false
		}
		a.dragging = false
		if a.inTimeline(msg.X, msg.Y) { // the selection ends where the button came up
			s.tl.ExtendSelect(a.timelinePoint(s, msg.X, msg.Y))
		}
		if text := s.tl.EndSelect(); text != "" {
			a.copySelection(text)
			return nil, true
		}
		return a.clickTimeline(s, msg.X, msg.Y), true // it was a click after all
	}
	return nil, false
}

// clickTimeline is the plain-click behaviour of the conversation pane: focus
// it, and fold or unfold a block when its header was hit.
func (a *App) clickTimeline(s *Session, x, y int) tea.Cmd {
	a.setFocus(focusTimeline)
	if i := s.tl.HeaderBlockAt(s.tl.YOffset() + y); i >= 0 {
		s.tl.ToggleCollapse(i)
	}
	return nil
}

// copyToClipboard is a seam so tests don't clobber the real clipboard.
var copyToClipboard = clipboard.WriteAll

// copySelection puts the selection on the system clipboard, reporting either
// outcome in the status bar rather than failing silently.
func (a *App) copySelection(text string) {
	if err := copyToClipboard(text); err != nil {
		a.note = "could not copy: " + err.Error()
		return
	}
	n := strings.Count(text, "\n") + 1
	if n == 1 {
		a.note = fmt.Sprintf("copied %d characters", len([]rune(text)))
		return
	}
	a.note = fmt.Sprintf("copied %d lines", n)
}

// handleClick makes the whole frame clickable: the sidebar switches or creates
// agents, the panes take focus, and a block or file header folds.
func (a *App) handleClick(msg tea.MouseMsg) tea.Cmd {
	// The status bar is the last row; its right edge holds the theme chip.
	if msg.Y == a.h-1 {
		if start, end := ThemeToggleRange(a.w); start > 0 && msg.X >= start && msg.X < end {
			ToggleMode()
			a.applyTheme()
			a.persist()
			return nil
		}
		return nil
	}
	// Below the panes is the input box — checked before the sidebar, which
	// only owns its columns down to the end of the pane row, not the input.
	if msg.Y >= a.lay.PaneH {
		if a.focus != focusInput {
			a.focus = focusInput
			return a.in.Focus()
		}
		return nil
	}
	if a.lay.ShowSidebar && msg.X < a.lay.SidebarW {
		a.clickSidebar(msg.Y)
		return nil
	}
	s := a.cur()
	if s == nil {
		return nil
	}
	if a.lay.ShowDiff && msg.X >= a.lay.SidebarW+a.lay.TimelineW {
		a.setFocus(focusDiff)
		// the diff pane has a border, so its first content row is one down
		if msg.Y >= 1 && msg.Y <= a.lay.PaneH-2 {
			s.dp.ToggleCollapse(s.dp.HeaderFileAt(s.dp.YOffset() + msg.Y - 1))
		}
		return nil
	}
	// The conversation pane is handled on release by routeDrag, so a press
	// that turns into a drag selects instead of folding.
	return nil
}

func (a *App) setFocus(f focusTarget) {
	if a.focus == f {
		return
	}
	a.focus = f
	a.in.Blur()
}

// clickSidebar turns a click at screen row y into a selection or a new agent.
func (a *App) clickSidebar(y int) {
	if y < 1 || y > a.lay.PaneH-2 {
		return // the box's own border
	}
	switch target, i := SidebarRowAt(len(a.sessions), y-1); target {
	case SidebarSession:
		a.selectSession(i)
	case SidebarNewAgent:
		a.openPicker()
	}
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
	if body := a.modalView(); body != "" {
		return lipgloss.PlaceHorizontal(a.w, lipgloss.Center, body,
			lipgloss.WithWhitespaceBackground(T.Bg)) + "\n" + a.statusLine()
	}

	s := a.cur()
	var panes []string
	if a.lay.ShowSidebar {
		panes = append(panes, pane(T.Muted).
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
			panes = append(panes, pane(c).
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
		Agent: s.Backend.Label(), Mode: string(s.Permission), Dir: s.Dir,
		Busy: s.busy, Spin: a.sp.View(), Cost: s.cost,
		Adds: s.diff.Additions, Dels: s.diff.Deletions, Note: a.note,
		Model:         s.Model,
		ContextTokens: s.ctxTokens, ContextWindow: s.ctxWindow, Limit: s.limit,
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
