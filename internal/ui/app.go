package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	// Hard floors: below these the pane is dropped no matter what the user
	// asked for, because the timeline would stop being readable. Above them
	// ctrl+b / ctrl+t decide. 70 keeps a 46-column timeline beside the sidebar.
	sidebarMinCols = 70
	diffMinCols    = 124 // the diff pane needs room left over after the sidebar
	diffDebounce   = 350 * time.Millisecond
)

// DiffView is how much room the diff gets: none, a column beside the
// conversation, or the whole frame.
type DiffView int

const (
	DiffHidden DiffView = iota
	DiffSide
	DiffFull
)

// Next cycles the three, which is what ctrl+t does.
func (v DiffView) Next() DiffView {
	if v == DiffFull {
		return DiffHidden
	}
	return v + 1
}

func (v DiffView) String() string {
	switch v {
	case DiffSide:
		return "diff pane"
	case DiffFull:
		return "diff full screen"
	}
	return "diff hidden"
}

type Layout struct {
	SidebarW, TimelineW, DiffW, PaneH int
	ShowSidebar, ShowDiff             bool
	FullDiff                          bool // the diff has the frame to itself
}

// ComputeLayout splits the terminal into sidebar | timeline | diff, dropping
// the optional panes as the terminal narrows so 80x24 stays usable. inputH is
// how tall the input box currently is: it grows with a multi-line draft, and
// the panes give up the rows.
func ComputeLayout(w, h, inputH int, wantSidebar bool, diff DiffView) Layout {
	paneH := h - inputH - StatusRows(h)
	if paneH < 3 {
		paneH = 3
	}
	l := Layout{PaneH: paneH, TimelineW: max(1, w)}
	if diff == DiffFull {
		// Full screen means exactly that: the sidebar and the conversation
		// stand aside, and the diff gets every column.
		return Layout{PaneH: paneH, ShowDiff: true, FullDiff: true, DiffW: max(1, w)}
	}
	wantDiff := diff == DiffSide
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
	// controls is the model / permissions button row above the input, nil when
	// it isn't showing.
	controls *Controls

	choices  *Choices     // the agent's own question, nil when it didn't ask one
	comp     *Completions // slash-command drop-up, nil when not completing
	compQ    string       // the word the list was built for
	compOffQ string       // the word esc dismissed; typing on re-opens the list
	compOff  bool

	// histIdx is where ↑ has walked back to in the focused agent's history,
	// -1 when the draft is the user's own; histDraft is what it interrupted.
	histIdx   int
	histDraft string

	lastKey time.Time // when the previous key arrived, for spotting a paste
	keyRun  int       // how many have arrived back-to-back since
	images  []string  // files behind the [Image #n] markers in the draft

	w, h        int
	lay         Layout
	wantSidebar bool
	diffView    DiffView
	dragging    dragPane // which pane a text selection is being dragged in
	dragAgent   int      // the sidebar row being moved, or noDrag
	dragMoved   bool     // whether the drag went anywhere, so a mere click is not announced
	// lastRow and lastClick are the previous press in the sidebar, for telling
	// a double-click (rename) from two separate ones.
	lastRow   int
	lastClick time.Time
	focus     focusTarget
	note      string
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
		wantSidebar: true, diffView: DiffSide, histIdx: -1,
		dragAgent: noDrag, lastRow: noDrag,
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
	cmds := []tea.Cmd{a.in.Focus(), heartbeat()}
	for _, s := range a.sessions {
		cmds = append(cmds, s.collectDiff())
	}
	return tea.Batch(cmds...)
}

// heartbeatMsg wakes an idle crema. Bubbletea only draws when a message
// arrives, so without one the usage gauges and their reset countdowns froze
// between keystrokes: the status-line bridge kept writing fresh numbers that
// nothing re-read until the user happened to touch something.
type heartbeatMsg struct{}

// heartbeatEvery is the idle redraw cadence. The bridge rewrites the numbers
// as often as an interactive session renders; ten seconds keeps a visibly
// moving bar and costs a file stat and a frame.
const heartbeatEvery = 10 * time.Second

func heartbeat() tea.Cmd {
	return tea.Tick(heartbeatEvery, func(time.Time) tea.Msg { return heartbeatMsg{} })
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		// Repaint the lot. A console that has been through a full-screen
		// toggle can leave the renderer's idea of the screen a line out of
		// step with the screen itself, and the line that goes missing is the
		// last one — the status bar.
		return a, tea.ClearScreen

	case tea.KeyMsg:
		if a.picker != nil {
			return a, a.updatePicker(msg)
		}
		return a.keyPress(msg)

	case tea.MouseMsg:
		return a, a.routeMouse(msg)

	case spinner.TickMsg:
		if !a.anyBusy() {
			return a, nil
		}
		var cmd tea.Cmd
		a.sp, cmd = a.sp.Update(msg)
		return a, cmd

	case heartbeatMsg:
		// The refresh itself happens in View, which this message exists to
		// cause; re-arm and let the frame do the reading.
		return a, heartbeat()

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
			s.endTurn()
			return a, nil
		}
		s.stream = msg.ch
		return a, waitForEvent(msg.ch, s.ID, msg.seq)

	case agentEventMsg:
		s := a.sessionByID(msg.sess)
		if s == nil || msg.seq != s.streamSeq {
			return a, nil
		}
		if msg.ev.Kind == agent.KindReady {
			// The backend has just said what it can be asked for. That list
			// outranks anything crema worked out for itself.
			s.cliCmds = msg.ev.Commands
			a.persist()
			return a, waitForEvent(s.stream, s.ID, msg.seq)
		}
		s.noteActivity(msg.ev) // what the working line says it is doing
		s.tl.AppendEvent(msg.ev)
		cmds := []tea.Cmd{waitForEvent(s.stream, s.ID, msg.seq)}
		switch msg.ev.Kind {
		case agent.KindTask:
			s.noteTask(msg.ev.Task)
		case agent.KindToolOutput:
			cmds = append(cmds, s.scheduleDiff())
		case agent.KindTurnEnd:
			s.noteResult(msg.ev.Result)
			a.persist() // a finished leg is worth surviving a crash
			cmds = append(cmds, s.collectDiff())
			// When the process is held open it stays put between turns, so
			// its result line is the end of the turn. A process-per-turn run
			// can produce several results — an async task revives it — so
			// there the exit is the only honest end.
			if s.persistent() && s.busy {
				cmds = append(cmds, a.finishTurn(s)...)
			}
		}
		return a, tea.Batch(cmds...)

	case streamClosedMsg:
		s := a.sessionByID(msg.sess)
		if s == nil || msg.seq != s.streamSeq {
			return a, nil
		}
		// A held-open conversation whose process ends has lost its process,
		// not its conversation: the CLI keeps the transcript and the next
		// message resumes it.
		if s.conv != nil {
			s.closeConv()
		}
		if !s.busy {
			return a, nil
		}
		return a, tea.Batch(a.finishTurn(s)...)

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
		return a, a.maybeCheckPR(s) // the diff just said which branch this is
	case prMsg:
		s := a.sessionByID(msg.sess)
		if s == nil {
			return a, nil
		}
		s.prChecking = false
		s.pr, s.prBranch, s.prAt = msg.pr, msg.branch, time.Now()
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

// controlsKey drives the button row. It keeps the row open after a change, so
// the model and the permission can both be set in one visit.
func (a *App) controlsKey(msg tea.KeyMsg) tea.Cmd {
	chosen, closed := a.controls.Update(msg)
	defer a.resize(a.w, a.h) // opening or closing the list moves the bottom strip
	return a.applyControl(chosen, closed)
}

func (a *App) applyControl(chosen *controlOption, closed bool) tea.Cmd {
	if closed {
		a.controls = nil
		a.focus = focusInput
		a.resize(a.w, a.h) // the status bar gets its place back
		return a.in.Focus()
	}
	s := a.cur()
	if chosen == nil || s == nil {
		return nil
	}
	switch chosen.kind {
	case controlModel:
		s.SetModel(chosen.model) // "" is a real choice here: the CLI's own default
	case controlPermission:
		s.SetPermission(chosen.perm)
	}
	s.tl.GotoEnd() // the change is written into the conversation; show it
	a.persist()
	return nil
}

// openControls raises the button row and takes the keyboard, so the very next
// key moves between the buttons rather than into the message.
func (a *App) openControls() tea.Cmd {
	s := a.cur()
	if s == nil {
		return nil
	}
	a.controls = NewControls(s)
	a.in.Blur()
	return nil
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
	if a.picker == nil {
		return ""
	}
	_, _, w, h := a.modalRect()
	return a.picker.View(w, h)
}

// modalRect is the open modal's on-screen box, computed identically by View
// and by the click handler.
func (a *App) modalRect() (x, y, w, h int) {
	w = min(a.w, 72)
	h = a.h - StatusRows(a.h) // everything above the status bar, input box included
	return (a.w - w) / 2, 0, w, h
}

// ctrlHeld and shiftHeld are the platform probes for a held modifier, as
// variables so tests can pretend the user is holding one.
var (
	ctrlHeld  = ctrlDown
	shiftHeld = shiftDown
)

func isLeftClick(m tea.MouseMsg) bool {
	// Wheel events are also "press", so the button has to be checked too.
	return m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft
}

// keyPress is the whole life of one keystroke in the main view: ctrl+backspace
// is recovered, the key is handled, the command list is rebuilt around the
// result, and the frame is re-laid if the input box changed height. The draft
// itself is only ever what the user typed — the drop-up shows the candidates
// and tab takes one, the way the CLIs' own menus work.
func (a *App) keyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	was := a.in.Height()
	gapMS, burst := a.noteKey()
	// The Windows console reader reports ctrl+backspace, ctrl+enter and
	// shift+enter as a plain backspace and a plain enter, throwing the
	// modifier away — but the OS still knows it was held. Rewriting them into
	// ^H and ^J is what the text area binds delete-word and newline to. An
	// enter has all its facts gathered and put to judgeEnter, one place for a
	// decision that used to be smeared across two; shiftHeld is still only
	// asked when ctrl was not the answer.
	held := ctrlHeld()
	switch {
	case msg.Type == tea.KeyBackspace && !msg.Alt && held:
		msg = tea.KeyMsg{Type: tea.KeyCtrlH}
	case msg.Type == tea.KeyEnter && !msg.Alt:
		f := enterFacts{
			paste: msg.Paste, ctrl: held, shift: !held && shiftHeld(),
			pressed: enterHeld(), burst: burst, gapMS: gapMS,
		}
		if newline, _ := judgeEnter(f); newline {
			msg = tea.KeyMsg{Type: tea.KeyCtrlJ}
		}
	}
	m, cmd := a.handleKey(msg)
	a.refreshCompletions()
	if a.in.Height() != was {
		a.resize(a.w, a.h)
	}
	return m, cmd
}

const (
	// pasteGap is how close together two keys have to arrive to be a replay
	// rather than a person typing. Nobody types two characters in 20ms, and
	// key autorepeat is slower than that too.
	pasteGap = 20 * time.Millisecond
	// pasteBurst is the outer limit of "still in the same burst of input".
	// Pasted text arrives all at once even when the app is too busy to keep up
	// with it; an enter that follows a pause is somebody deciding to send.
	pasteBurst = 200 * time.Millisecond
)

// timeNow and enterHeld are what the paste heuristic reads, as variables so
// tests can say what a key should look like.
var (
	timeNow   = time.Now
	enterHeld = enterDown
)

// noteKey records one keystroke's arrival and reports the timing facts
// judgeEnter wants: how long since the key before it, and how many keys deep
// the current burst is.
//
// The timing matters because the Windows console has no bracketed paste: it
// replays a paste into the input buffer as ordinary key events, so a newline
// in the middle of one is indistinguishable from the user pressing enter —
// which is how a paste used to send its own first line. Keys arriving faster
// than anyone can type is one giveaway; the key never having been pressed is
// the other. judgeEnter weighs them.
func (a *App) noteKey() (gapMS, burst int) {
	now := timeNow()
	gap, first := now.Sub(a.lastKey), a.lastKey.IsZero()
	a.lastKey = now
	if first || gap >= pasteGap {
		a.keyRun = 0
	} else {
		a.keyRun++
	}
	if first {
		return -1, a.keyRun
	}
	if gap > time.Hour {
		gap = time.Hour // gapMS only has to say "a pause"; keep the int honest
	}
	return int(gap / time.Millisecond), a.keyRun
}

// refreshCompletions rebuilds the drop-up from the draft. It runs after every
// key, so the list follows what has been typed — whether that is a "/" command
// or an "@" file.
func (a *App) refreshCompletions() {
	if a.focus != focusInput || a.controls != nil || a.picker != nil {
		a.comp = nil
		return
	}
	if a.browsing() {
		// Recalling "/clear" is not typing it: the command list would open on
		// top of the history and take the next ↑ for itself.
		a.comp = nil
		return
	}
	kind, q, at, ok := completionTrigger(a.in.Value())
	if !ok {
		a.comp, a.compOff = nil, false // not naming anything: re-arm for the next
		return
	}
	key := kind.trigger() + q
	if a.compOff && key == a.compOffQ {
		a.comp = nil
		return
	}
	a.compOff = false
	if a.comp != nil && key == a.compQ {
		return // same word: keep the list, and the row the user moved to
	}
	a.compQ, a.comp = key, nil
	s := a.cur()
	if s == nil {
		return
	}
	if kind == completeFile {
		a.comp = NewCompletions(kind, at, fileItems(s.Files()), q)
		return
	}
	a.comp = NewCompletions(kind, at, commandItems(allCommands(s)), q)
}

// completionKey lets the open drop-up claim the keys it needs — including
// enter, tab and esc, which mean something else the rest of the time.
func (a *App) completionKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up":
		a.comp.Move(-1)
		return nil, true
	case "down":
		a.comp.Move(1)
		return nil, true
	case "tab", "enter":
		return a.acceptCompletion(), true
	case "esc":
		a.compOffQ, a.compOff, a.comp = a.compQ, true, nil
		return nil, true
	}
	return nil, false
}

// offerChoices puts a picker over the input when the agent finished by asking
// something and listing the answers. Only for the agent you are looking at,
// and only when you haven't started writing: a background agent must not take
// the keyboard, and a draft in progress outranks a suggestion.
func (a *App) offerChoices(s *Session) {
	a.choices = nil
	if s != a.cur() || a.in.Value() != "" || a.focus != focusInput {
		return
	}
	a.choices = NewChoices(ParseChoices(s.tl.LastText()))
}

// choiceKey lets the open picker claim the keys it needs. Everything else
// dismisses it, so answering in your own words is always one keystroke away.
func (a *App) choiceKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up":
		a.choices.Move(-1)
		return nil, true
	case "down":
		a.choices.Move(1)
		return nil, true
	case "enter":
		return a.answerChoice(), true
	case "esc":
		a.choices = nil
		return nil, true
	}
	a.choices = nil // anything else: the user is writing their own answer
	return nil, false
}

// answerChoice sends the highlighted option as the next message, which is
// exactly what typing it would have done — the same door, so an option that
// is a command runs as one and an answer during a busy turn waits in line.
func (a *App) answerChoice() tea.Cmd {
	answer := a.choices.Selected()
	a.choices = nil
	if a.cur() == nil {
		return nil
	}
	return a.sendText(answer)
}

// sendText is the one door a message leaves through, whether it was typed and
// entered or picked from the agent's options: remembered for ↑, offered to
// crema's own commands, queued behind a busy turn, and only then a turn.
func (a *App) sendText(text string) tea.Cmd {
	s := a.cur()
	if s == nil {
		return nil
	}
	s.remember(text) // ↑ finds it again, whatever it turns out to be
	a.endBrowsing()
	// The CLIs' own /clear, /model and friends only exist in their
	// interactive interfaces; headless, they are just prompts. Crema does
	// the ones it can do itself rather than paying for a shrug.
	if cmd, handled := a.runBuiltin(s, text); handled {
		a.in.Reset()
		a.images = nil
		return cmd
	}
	prompt := expandImages(text, a.images)
	images := a.images
	a.images = nil // the markers went with the draft
	a.in.Reset()
	if s.busy {
		// One turn is one run of the CLI, so this waits rather than
		// interrupting — and goes on its own the moment the turn ends.
		n := s.enqueue(text, prompt, images)
		a.note = fmt.Sprintf("queued (%d) — it goes when this turn finishes", n)
		return nil
	}
	a.note = ""
	return tea.Batch(s.startTurn(text, prompt), a.sp.Tick)
}

// isTyping reports whether a key is a plain character meant for the message
// being written, rather than a shortcut or a way of moving around. Ctrl and
// alt combinations arrive as their own key types, so they are never this.
func isTyping(msg tea.KeyMsg) bool {
	if msg.Alt {
		return false
	}
	return msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace
}

// focusInput puts the focus back on the message box, doing nothing when it is
// already there.
func (a *App) focusInput() tea.Cmd {
	if a.focus == focusInput {
		return nil
	}
	a.focus = focusInput
	return a.in.Focus()
}

// acceptCompletion writes the chosen name into the draft with a trailing
// space, leaving the user on what comes after it. Sending is still a separate
// enter, so a command can be completed and then explained, and a file can be
// mentioned in the middle of a sentence.
func (a *App) acceptCompletion() tea.Cmd {
	a.in.SetValue(a.comp.Apply(a.in.Value()))
	a.comp = nil
	return nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The diff's find box owns the keyboard while it is up, before anything
	// else looks: what you type there is a search, not a message. Keys it has
	// no use for — page down, the pane toggles — fall through as usual.
	if s := a.cur(); s != nil && a.focus == focusDiff {
		if s.dp.Searching() && s.dp.SearchKey(msg) {
			return a, nil
		}
		if msg.String() == "/" && !s.dp.Searching() {
			s.dp.StartSearch() // the pager gesture, since the diff has focus
			return a, nil
		}
	}
	if a.controls != nil {
		// The button row has the keyboard: arrows move between the buttons and
		// through a list, rather than into the message. So does a number while
		// a list is open, since the rows are numbered — but not while only the
		// buttons show, where a bare digit is the start of a message.
		if !isTyping(msg) || (a.controls.Open() && digit(msg) > 0) {
			return a, a.controlsKey(msg)
		}
		// Typing is a message: the row stands aside and the key goes to the
		// draft. The input is re-focused directly — a.focus never left it, so
		// focusInput would think there was nothing to do.
		a.controls = nil
		a.resize(a.w, a.h) // the status bar gets its place back
		return a, tea.Batch(a.in.Focus(), a.routeToFocus(msg))
	}
	if msg.Paste || isTyping(msg) {
		// Typing is always meant for the message being written, wherever the
		// focus happens to be. Take the focus back and let the key through.
		a.choices = nil // writing your own answer beats the offered ones
		a.endBrowsing() // and the draft stops being the history's to write
		cmd := a.focusInput()
		return a, tea.Batch(cmd, a.routeToFocus(msg))
	}
	if a.choices != nil {
		if cmd, handled := a.choiceKey(msg); handled {
			return a, cmd
		}
	}
	if a.comp != nil {
		if cmd, handled := a.completionKey(msg); handled {
			return a, cmd
		}
	}
	switch msg.String() {
	case "ctrl+c":
		// Copy, not quit: the point of selecting text is to take it with you,
		// and losing the session to the reflex of copying it is a bad trade.
		if text := a.selectedText(); text != "" {
			a.copySelection(text)
			return a, nil
		}
		a.note = "nothing selected — drag to select · esc cancels the turn · ctrl+q quits"
		return a, nil
	case "ctrl+v":
		return a, a.pasteFromClipboard()
	case "ctrl+q":
		a.persist()
		for _, s := range a.sessions {
			s.close()
		}
		return a, tea.Quit
	case "ctrl+n":
		a.openPicker()
		return a, nil
	case "ctrl+p":
		return a, a.openControls()
	case "up":
		// In the file browser the arrows walk the list; pgup/pgdn scroll the
		// diff itself.
		if s := a.cur(); s != nil && a.focus == focusDiff && s.dp.Browsing() {
			s.dp.SelectFile(-1)
			return a, nil
		}
		// ↑ walks back through what you have asked this agent, the way a shell
		// does. A multi-line draft keeps it as cursor movement.
		if a.arrowsAreForHistory() && a.recallPrev() {
			return a, nil
		}
	case "down":
		if s := a.cur(); s != nil && a.focus == focusDiff && s.dp.Browsing() {
			s.dp.SelectFile(1)
			return a, nil
		}
		// ↓ walks the history forward again, and once it is back at the draft
		// it interrupted, reaches the buttons above the input — the row it
		// lands on navigates with the same keys, so the gesture keeps going.
		if a.arrowsAreForHistory() {
			if a.recallNext() {
				return a, nil
			}
			return a, a.openControls()
		}
	case "ctrl+w":
		return a, a.closeSession()
	case "esc":
		if s := a.cur(); s != nil {
			if a.selectedText() != "" {
				// drop the highlight before touching the turn
				s.tl.ClearSelection()
				s.dp.ClearSelection()
				return a, nil
			}
			// Cancelling means cancelling: anything queued behind this turn
			// was written expecting it to finish, so it goes too rather than
			// firing off the moment the turn dies.
			if n := s.dropQueue(); n > 0 {
				a.note = fmt.Sprintf("canceling — %d queued message(s) dropped", n)
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
		return a, a.cycleDiff()
	case "ctrl+f":
		return a, a.startDiffSearch()
	case "ctrl+l":
		ToggleMode() // the status-bar chip shows the result, so no note needed
		a.applyTheme()
		a.persist()
		return a, nil
	case "ctrl+r":
		if s := a.cur(); s != nil {
			s.Reload() // commands and files change while crema is running
			return a, s.collectDiff()
		}
		return a, nil
	case "ctrl+o":
		return a, a.cycleFocus()
	case "enter":
		text := strings.TrimSpace(a.in.Value())
		if text == "" {
			return a, nil
		}
		// A written draft makes enter a send wherever the focus wandered —
		// usually into the conversation, to copy something the agent said.
		// It used to be silently ignored there, which read as the agent not
		// answering. An empty box keeps enter quiet on the other panes.
		return a, tea.Batch(a.focusInput(), a.sendText(text))
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
	a.endBrowsing() // another agent, another history
	a.note = ""
	a.resize(a.w, a.h)
}

// closeSession ends the focused agent. Closing the last one quits.
func (a *App) closeSession() tea.Cmd { return a.closeSessionAt(a.active) }

// closeSessionAt ends one agent by position, which is what the × on its row
// does. Closing the last one quits, since crema without an agent has nothing
// to show — the same thing ctrl+w has always done.
func (a *App) closeSessionAt(i int) tea.Cmd {
	if i < 0 || i >= len(a.sessions) {
		return nil
	}
	s := a.sessions[i]
	s.close()
	a.sessions = append(a.sessions[:i], a.sessions[i+1:]...)
	if len(a.sessions) == 0 {
		a.persist()
		return tea.Quit
	}
	// Keep looking at the same agent when one before it goes.
	if i < a.active || a.active >= len(a.sessions) {
		a.active = max(0, min(a.active-1, len(a.sessions)-1))
	}
	a.note = "closed " + s.Title()
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
	if a.picker != nil {
		if !isLeftClick(msg) {
			return nil
		}
		x, y, w, _ := a.modalRect()
		if msg.X < x+2 || msg.X >= x+w-2 || msg.Y < y+1 {
			return nil // border, padding, or outside the modal
		}
		return a.finishPicker(a.picker.ClickRow(msg.Y - (y + 1)))
	}
	// The sidebar comes first: a press there may be the start of a drag that
	// reorders the agents, and that has to be decided before the panes treat
	// the same movement as a text selection.
	if cmd, handled := a.sidebarMouse(msg); handled {
		return cmd
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

// bottomRows is how tall the frame's bottom strip is: the status bar,
// or the model/permission list that replaces it while one is open.
func (a *App) bottomRows() int {
	if a.controls != nil && a.controls.Open() {
		return a.controls.ListHeight()
	}
	return StatusRows(a.h)
}

// dropUpHeight is how many rows the box floating over the bottom of the panes
// takes, 0 when there isn't one. Only one is ever up, in this order: what you
// are typing beats what the agent asked, which beats what is waiting to be
// sent.
func (a *App) dropUpHeight() int {
	switch {
	case a.controls != nil:
		return a.controls.Height()
	case a.comp != nil:
		return a.comp.Height()
	case a.choices != nil:
		return a.choices.Height()
	}
	return 0
}

// unqueue takes a waiting message back out and puts it in the input box, where
// it can be edited and sent again — which is the only reason to want it back.
//
// A draft already being written is never overwritten: the message stays where
// it is and says so, so nothing is lost either way.
func (a *App) unqueue(i int) {
	s := a.cur()
	if s == nil || i < 0 || i >= len(s.queued) {
		return
	}
	if strings.TrimSpace(a.in.Value()) != "" {
		a.note = "finish or clear the draft first — then click again to take that one back"
		return
	}
	q := s.queued[i]
	s.queued = append(s.queued[:i], s.queued[i+1:]...)

	was := a.in.Height()
	a.in.SetValue(q.shown)
	a.images = q.images // the [Image #n] in it point at these again
	a.endBrowsing()
	if a.in.Height() != was {
		a.resize(a.w, a.h) // a click doesn't go through keyPress, which does this
	}
	a.note = "back in the box — edit it and press enter"
}

// overCompletions reports whether a screen row is covered by that box.
func (a *App) overCompletions(y int) bool {
	h := a.dropUpHeight()
	return h > 0 && y >= a.lay.PaneH-h && y < a.lay.PaneH
}

// inTimeline reports whether a screen position is inside the conversation pane.
func (a *App) inTimeline(x, y int) bool {
	if a.overCompletions(y) {
		return false // clicks there belong to the drop-up, not to a selection
	}
	return y >= 0 && y < a.lay.PaneH &&
		x >= a.lay.SidebarW && x < a.lay.SidebarW+a.lay.TimelineW
}

// inDiff is the same for the diff pane, whose rounded border is not part of
// what it shows: a drag starts one row down and one column in.
func (a *App) inDiff(x, y int) bool {
	if !a.lay.ShowDiff || a.overCompletions(y) {
		return false
	}
	left := a.lay.SidebarW + a.lay.TimelineW
	return y >= 1 && y < a.lay.PaneH-1 && x >= left+1 && x < left+a.lay.DiffW-1
}

// timelinePoint converts screen coordinates to a position in the conversation.
func (a *App) timelinePoint(s *Session, x, y int) (line, col int) {
	return s.tl.YOffset() + y, x - a.lay.SidebarW
}

// diffPoint does the same for the diff, allowing for its border and for the
// header row inside it.
func (a *App) diffPoint(s *Session, x, y int) (line, col int) {
	return s.dp.YOffset() + y - 2, s.dp.BodyCol(a.diffCol(x))
}

// diffCol is a screen column as a column of the pane's content, inside the
// border.
func (a *App) diffCol(x int) int {
	return x - (a.lay.SidebarW + a.lay.TimelineW) - 1
}

// dragPane is which pane a drag belongs to; a drag stays with the pane it
// started in even when the pointer wanders out of it.
type dragPane int

const (
	dragNone dragPane = iota
	dragTimeline
	dragDiff
)

// routeDrag implements press/drag/release selection in the two panes that show
// text worth copying. It reports handled=true only for events it consumed, so
// a plain click still falls through to the button behaviour.
func (a *App) routeDrag(msg tea.MouseMsg) (tea.Cmd, bool) {
	s := a.cur()
	if s == nil || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}
	switch msg.Action {
	case tea.MouseActionPress:
		switch {
		case a.inTimeline(msg.X, msg.Y):
			a.dragging = dragTimeline
			s.dp.ClearSelection()
			s.tl.BeginSelect(a.timelinePoint(s, msg.X, msg.Y))
		case a.inDiff(msg.X, msg.Y):
			a.dragging = dragDiff
			s.tl.ClearSelection()
			s.dp.BeginSelect(a.diffPoint(s, msg.X, msg.Y))
		default:
			s.tl.ClearSelection() // clicking elsewhere drops the highlight
			s.dp.ClearSelection()
			return nil, false
		}
		// Consume the press. Acting on it now would fold a block out from
		// under a drag that was only just beginning; the click behaviour runs
		// on release instead, once we know it wasn't a drag.
		return nil, true

	case tea.MouseActionMotion:
		y := min(max(msg.Y, 0), a.lay.PaneH-1)
		switch a.dragging {
		case dragTimeline:
			s.tl.ExtendSelect(a.timelinePoint(s, msg.X, y))
		case dragDiff:
			s.dp.ExtendSelect(a.diffPoint(s, msg.X, y))
		default:
			return nil, false
		}
		return nil, true

	case tea.MouseActionRelease:
		pane := a.dragging
		a.dragging = dragNone
		switch pane {
		case dragTimeline:
			if a.inTimeline(msg.X, msg.Y) { // the selection ends where the button came up
				s.tl.ExtendSelect(a.timelinePoint(s, msg.X, msg.Y))
			}
			if text := s.tl.EndSelect(); text != "" {
				a.copySelection(text)
				return nil, true
			}
			return a.clickTimeline(s, msg.X, msg.Y), true // it was a click after all
		case dragDiff:
			if a.inDiff(msg.X, msg.Y) {
				s.dp.ExtendSelect(a.diffPoint(s, msg.X, msg.Y))
			}
			if text := s.dp.EndSelect(); text != "" {
				a.copySelection(text)
				return nil, true
			}
			return a.clickDiff(s, msg.X, msg.Y), true
		}
		return nil, false
	}
	return nil, false
}

// clickTimeline is the plain-click behaviour of the conversation pane: focus
// it, and fold or unfold a block when its header was hit.
func (a *App) clickTimeline(s *Session, x, y int) tea.Cmd {
	line := s.tl.YOffset() + y
	if i := s.tl.PendingAt(line); i >= 0 {
		// The waiting messages are drawn at the end of the conversation, so a
		// click there is a click on one of them.
		a.unqueue(i)
		return nil
	}
	a.setFocus(focusTimeline)
	if i := s.tl.HeaderBlockAt(line); i >= 0 {
		s.tl.ToggleCollapse(i)
		return nil
	}
	// A click that lands on a URL opens it — crema has the mouse, so the
	// terminal's own ctrl+click was never going to arrive.
	if _, col := a.timelinePoint(s, x, y); col >= 0 {
		if u := s.tl.LinkAt(line, col); u != "" {
			if err := openURL(u); err != nil {
				a.note = "could not open " + u + ": " + err.Error()
			} else {
				a.note = "opened " + u
			}
		}
	}
	return nil
}

// clickDiff is the same for the diff pane, which has three things to hit: the
// size buttons in its header, a file in the browser's list, and a file header
// in the stacked view.
func (a *App) clickDiff(s *Session, x, y int) tea.Cmd {
	col := a.diffCol(x)
	if y == 1 { // the pane's own header, inside its border
		if v, ok := s.dp.HeaderModeAt(col); ok {
			return a.setDiffView(v)
		}
		return nil
	}
	a.setFocus(focusDiff)
	if s.dp.InFileList(col) {
		s.dp.SelectFileAt(y - 2)
		return nil
	}
	line, _ := a.diffPoint(s, x, y)
	s.dp.ToggleCollapse(s.dp.HeaderFileAt(line))
	return nil
}

// setDiffView takes the diff straight to one of its three sizes, which is what
// the buttons on its header do — ctrl+t still cycles.
func (a *App) setDiffView(v DiffView) tea.Cmd {
	if a.diffView == v {
		return nil
	}
	a.diffView = v
	a.note = v.String()
	a.resize(a.w, a.h)
	return a.focusForDiff()
}

// imageDir is where pasted pictures are kept: out of the way, and out of the
// project — a screenshot is a question, not a file the repository wants.
func imageDir() string { return filepath.Join(os.TempDir(), "crema-images") }

// readClipboard and clipboardImage are seams so tests don't need a clipboard.
var (
	readClipboard = clipboard.ReadAll
	readClipImage = clipboardImage
)

// imageMarker is what a pasted picture looks like in the draft. A path is a
// mouthful of temp directory that says nothing, so the draft gets a label and
// the agent gets the file — the same trade the CLIs' own input boxes make.
func imageMarker(n int) string { return fmt.Sprintf("[Image #%d]", n) }

var imageMarkerPattern = regexp.MustCompile(`\[Image #(\d+)\]`)

// expandImages swaps each marker for the file it stands for, which is what
// actually goes to the agent. A marker with nothing behind it — typed by hand,
// or left over from an image whose paste was undone — is left as it is.
func expandImages(draft string, images []string) string {
	return imageMarkerPattern.ReplaceAllStringFunc(draft, func(m string) string {
		n, err := strconv.Atoi(imageMarkerPattern.FindStringSubmatch(m)[1])
		if err != nil || n < 1 || n > len(images) {
			return m
		}
		path := images[n-1]
		if strings.ContainsAny(path, " \t") {
			return `"` + path + `"` // a folder with a space in it
		}
		return path
	})
}

// pasteFromClipboard puts whatever was copied into the draft. A picture is
// written out and stands in the draft as [Image #1], expanded to its path on
// the way to the agent: both CLIs read an image file when the prompt names
// one. Text is inserted as text, so ctrl+v still does the obvious thing.
func (a *App) pasteFromClipboard() tea.Cmd {
	cmd := a.focusInput()
	if path, err := readClipImage(imageDir()); err == nil && path != "" {
		a.images = append(a.images, path)
		a.in.Insert(imageMarker(len(a.images)) + " ")
		a.note = fmt.Sprintf("%s attached — %s", imageMarker(len(a.images)), filepath.Base(path))
		a.refreshCompletions()
		return cmd
	} else if err != nil && !errors.Is(err, errNoImage) {
		a.note = "could not read the image: " + err.Error()
		return cmd
	}
	text, err := readClipboard()
	if err != nil {
		a.note = "nothing to paste: " + err.Error()
		return cmd
	}
	if text == "" {
		a.note = "the clipboard is empty"
		return cmd
	}
	a.in.Insert(text)
	a.refreshCompletions()
	return cmd
}

// selectedText is whatever is highlighted, in whichever pane holds it. Only
// one ever does: starting a drag in one clears the other.
func (a *App) selectedText() string {
	s := a.cur()
	if s == nil {
		return ""
	}
	if s.tl.HasSelection() {
		return s.tl.SelectedText()
	}
	if s.dp.HasSelection() {
		return s.dp.SelectedText()
	}
	return ""
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
	// While a model/permission list holds the bottom strip, clicks there pick
	// values — and the strip is only that, so nothing else can be under it.
	if a.controls != nil && a.controls.Open() {
		if top := a.h - a.controls.ListHeight(); msg.Y >= top {
			chosen, hit := a.controls.ClickListRow(msg.Y-top, msg.X)
			cmd := a.applyControl(chosen, false)
			_ = hit
			a.resize(a.w, a.h)
			return cmd
		}
	}
	// The status bar is the last row; its right edge holds the chips.
	if msg.Y == a.h-1 {
		if start, end := ThemeToggleRange(a.w); start > 0 && msg.X >= start && msg.X < end {
			ToggleMode()
			a.applyTheme()
			a.persist()
			return nil
		}
		if s := a.cur(); s != nil && s.pr != nil {
			if start, end := PRRange(a.w, a.diffView, s.pr); start > 0 && msg.X >= start && msg.X < end {
				if err := openURL(s.pr.URL); err != nil {
					a.note = "could not open the PR: " + err.Error()
				} else {
					a.note = "opened " + s.pr.URL
				}
				return nil
			}
		}
		// While the diff is hidden it has no header to carry its own buttons,
		// so the way back sits here instead.
		if start, end := ShowDiffRange(a.w, a.diffView); start > 0 && msg.X >= start && msg.X < end {
			return a.setDiffView(DiffSide)
		}
		return nil
	}
	if a.overCompletions(msg.Y) {
		row := msg.Y - (a.lay.PaneH - a.dropUpHeight())
		switch {
		case a.controls != nil:
			chosen, hit := a.controls.ClickRow(row, msg.X)
			if hit {
				cmd := a.applyControl(chosen, false)
				a.resize(a.w, a.h) // a click can open or close the bottom strip
				return cmd
			}
		case a.comp != nil:
			if a.comp.Click(row) {
				return a.acceptCompletion()
			}
		case a.choices != nil:
			if a.choices.Click(row) {
				return a.answerChoice()
			}
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
		return a.clickSidebar(msg.X, msg.Y)
	}
	s := a.cur()
	if s == nil {
		return nil
	}
	if a.lay.ShowDiff && msg.X >= a.lay.SidebarW+a.lay.TimelineW {
		a.setFocus(focusDiff) // its border; the inside is routeDrag's on release
		return nil
	}
	// Both text panes are handled on release by routeDrag, so a press that
	// turns into a drag selects instead of folding.
	return nil
}

func (a *App) setFocus(f focusTarget) {
	if a.focus == f {
		return
	}
	a.focus = f
	a.in.Blur()
}

// clickSidebar turns a click into a selection, a closed agent, or a new one.
// The sidebar's content sits one row and one column inside its border.
func (a *App) clickSidebar(x, y int) tea.Cmd {
	if y < 1 || y > a.lay.PaneH-2 {
		return nil // the box's own border
	}
	switch target, i := SidebarRowAt(len(a.sessions), y-1); target {
	case SidebarSession:
		if x-1 >= SidebarCloseCol(a.lay.SidebarW-2) {
			return a.closeSessionAt(i)
		}
		a.selectSession(i)
	case SidebarNewAgent:
		a.openPicker()
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
	a.in.SetWidth(w) // before the layout: wrapping decides how tall the box is
	a.lay = ComputeLayout(w, h, a.in.Height(), a.wantSidebar, a.diffView)
	// An open model/permission list takes the status bar's place and is
	// taller than it; the panes give up the difference.
	if extra := a.bottomRows() - StatusRows(h); extra > 0 {
		a.lay.PaneH = max(3, a.lay.PaneH-extra)
	}
	for _, s := range a.sessions {
		s.SetSize(a.lay.TimelineW, a.lay.DiffW, a.lay.PaneH)
		s.dp.SetView(a.diffView)
	}
	if !a.lay.ShowDiff && a.focus == focusDiff {
		a.focus = focusInput
		a.in.Focus()
	}
}

// cycleDiff moves the diff on to its next size. Three stops: hidden, a column
// beside the conversation, the whole screen — where the split view lives. Both
// ctrl+t and the status-bar chip come through here.
func (a *App) cycleDiff() tea.Cmd {
	a.diffView = a.diffView.Next()
	a.note = a.diffView.String()
	a.resize(a.w, a.h)
	return a.focusForDiff()
}

// startDiffSearch puts the cursor in the diff's find box. Asking to search the
// diff is asking to see it, so a hidden pane is opened first — full screen if
// the terminal is too narrow to put it beside the conversation.
func (a *App) startDiffSearch() tea.Cmd {
	s := a.cur()
	if s == nil {
		return nil
	}
	if !a.lay.ShowDiff {
		a.diffView = DiffSide
		a.resize(a.w, a.h)
	}
	if !a.lay.ShowDiff {
		a.diffView = DiffFull
		a.resize(a.w, a.h)
	}
	a.setFocus(focusDiff)
	s.dp.StartSearch()
	a.note = "find in the diff — type, ↑↓ to move, esc to close"
	return nil
}

// focusForDiff hands the focus to the diff when it takes the screen — there is
// nothing else to scroll — and gives it back to the input when it lets go.
func (a *App) focusForDiff() tea.Cmd {
	if a.lay.FullDiff {
		a.focus = focusDiff
		a.in.Blur()
		return nil
	}
	if a.focus == focusDiff {
		return a.focusInput()
	}
	return nil
}

func (a *App) View() string {
	if a.w == 0 || a.h == 0 {
		return "starting crema…"
	}
	if body := a.modalView(); body != "" {
		return fitFrame(lipgloss.PlaceHorizontal(a.w, lipgloss.Center, body,
			lipgloss.WithWhitespaceBackground(T.Bg))+"\n"+a.statusLine(), a.h)
	}

	s := a.cur()
	var panes []string
	if a.lay.ShowSidebar {
		panes = append(panes, pane(T.Muted).
			Width(a.lay.SidebarW-2).Height(a.lay.PaneH-2).
			Render(RenderSidebar(a.sessions, a.active, a.dragAgent, a.sp.View(), a.lay.SidebarW-2, a.lay.PaneH-2)))
	}
	if s != nil {
		// Every agent's conversation ends with whatever it has waiting, and the
		// queue changes from half a dozen places — draining it, cancelling it,
		// taking one back. Reconciling here is the one spot none of them can
		// forget; SetPending does nothing when nothing has changed.
		for _, x := range a.sessions {
			x.maybeRefreshLimits()
			x.maybeCloseIdleConv()
			x.tl.SetStatus(x.workingLine(a.sp.View(), a.lay.TimelineW))
			x.tl.SetPending(x.Queued())
		}
		if !a.lay.FullDiff {
			panes = append(panes, s.tl.View())
		}
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
	switch {
	case a.controls != nil:
		main = overlayBottom(main, a.controls.View(a.w))
	case a.comp != nil:
		main = overlayBottom(main, a.comp.View(a.w))
	case a.choices != nil:
		main = overlayBottom(main, a.choices.View(a.w))
	}
	bottom := a.statusLine()
	if a.controls != nil && a.controls.Open() {
		bottom = a.controls.ListView(a.w)
	}
	return fitFrame(strings.Join([]string{main, a.in.View(), bottom}, "\n"), a.h)
}

// fitFrame makes the frame exactly as tall as the terminal. The panes are
// sized to add up on their own, but a pane that miscounts by a line — a wrap
// nobody predicted, a resize arriving mid-render — would push the bottom line
// off the screen, and the bottom line is the status bar with its buttons on
// it. Anything over is trimmed from the top, where the conversation can spare
// a row and scroll it back.
func fitFrame(frame string, h int) string {
	if h <= 0 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	switch {
	case len(lines) > h:
		lines = lines[len(lines)-h:]
	case len(lines) < h:
		lines = append(make([]string, h-len(lines)), lines...)
	}
	return strings.Join(lines, "\n")
}

func (a *App) statusLine() string {
	s := a.cur()
	if s == nil {
		return RenderStatus(StatusData{Agent: "no agents", Mode: "—", Note: a.note},
			a.w, StatusRows(a.h))
	}
	d := StatusData{
		Agent: s.Backend.Label(), Mode: s.Permission.Label(), Dir: s.Dir, Diff: a.diffView,
		Busy: s.busy, Spin: a.sp.View(), Cost: s.cost,
		Adds: s.diff.Additions, Dels: s.diff.Deletions, Note: a.note,
		Branch: s.diff.Branch, Untracked: s.diff.Untracked, PR: s.pr,
		Model:         s.Model,
		ContextTokens: s.ctxTokens, ContextWindow: s.ctxWindow, Limits: s.limits,
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
	return RenderStatus(d, a.w, StatusRows(a.h))
}

// finishTurn is everything that happens when a turn is over, wherever the
// end came from: the process exiting, or — when it is held open across turns
// — its own result line.
func (a *App) finishTurn(s *Session) []tea.Cmd {
	ended, canceled := s.lastResult != nil, s.canceled
	s.endTurn()
	switch {
	case canceled:
		// The conversation was closed under it; the CLI never got to say so.
	case !ended: // adapters guarantee a result; this is the belt-and-braces path
		s.tl.Append(Block{Kind: BlockError, Text: "the agent stream ended without finishing the turn"})
		return nil
	}
	if s.compacting {
		a.finishCompact(s) // the reply was a summary, not an answer
	}
	a.persist()
	cmds := []tea.Cmd{s.collectDiff()}
	// Whatever was typed while this turn ran goes now. Only then is there any
	// point offering the agent's own question — a queued message is already
	// the answer to it.
	if q, ok := s.nextQueued(); ok {
		cmds = append(cmds, s.startTurn(q.shown, q.prompt), a.sp.Tick)
	} else {
		a.offerChoices(s)
	}
	return cmds
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
