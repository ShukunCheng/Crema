package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/gitdiff"
	tea "github.com/charmbracelet/bubbletea"
)

// Session is one open agent: a backend bound to a working directory, with its
// own conversation, its own diff pane, and its own in-flight turn. Sessions run
// concurrently — every message carries the session id it belongs to so events
// from a background agent land in the right timeline.
type Session struct {
	ID      int
	Backend agent.Agent
	Dir     string
	// Permission and Model are per-agent settings, changed with ctrl+p and
	// saved between runs. Name is what you called this agent, empty until you
	// call it something.
	Permission agent.PermissionMode
	// AutoCompact folds the conversation up before it grows expensive. On by
	// default, which is what the CLI's own interactive sessions do.
	AutoCompact bool
	Model       string
	Name        string

	tl *Timeline
	dp *DiffPanel

	busy      bool
	turnStart time.Time
	// activity and turnOut drive the working line under the conversation: what
	// the agent was last seen doing, and how much it has written this turn.
	activity   string
	turnOut    int64
	turnEvents int // events this turn produced; zero at the end means it said nothing
	// tasks is what the backend has said about background work — subagents
	// and backgrounded commands — newest report merged over the last. /tasks
	// reads it; the working line counts the running ones.
	tasks []agent.TaskUpdate
	// lastResult is the newest result line of the running turn. A turn can
	// end more than once — an async task continues it — so results are
	// absorbed as they come and the stream closing is what ends the turn.
	lastResult *agent.TurnResult
	stream     <-chan agent.Event
	streamSeq  int
	cancel     context.CancelFunc
	agentSID   string // backend session id, for resume
	cost       float64
	ctxTokens  int64 // context occupancy reported by the last turn
	ctxWindow  int64
	// limits are the account's allowance windows. The stream names the window
	// and its reset; the percentage, when there is one, comes from the
	// backend's own usage cache.
	limits     []agent.RateLimit
	turnLimits []agent.RateLimit // the last turn's own report, a fallback between file writes
	// pr is the branch's pull request, from gh, cached per branch. prAt is
	// when it was last asked; nil pr with a fresh prAt means "asked, none".
	// conv is the agent's CLI process, held open across turns when the
	// backend can do that. Nil means the next turn opens one — or, for a
	// backend without the mode, that every turn is its own process.
	conv     agent.Conversation
	convStop context.CancelFunc
	// convAt is when the conversation last carried a turn: a process held
	// open past the prompt cache's own lifetime is holding nothing worth
	// having, so it is let go.
	convAt   time.Time
	canceled bool

	pr         *PRInfo
	prBranch   string
	prAt       time.Time
	prChecking bool
	turnLimit  *agent.RateLimit // the newest rate_limit_event, kept across refreshes
	limitsAt   time.Time        // when the allowance was last read off disk
	lastOpts   agent.RunOptions

	diff        gitdiff.DiffSet
	diffSeq     int
	diffApplied int

	// queued holds messages typed while this agent was working. A turn is one
	// run of the CLI and there is no way into it once it starts, so the next
	// message waits here and goes the moment the turn ends.
	queued []queuedTurn

	// compacting marks the turn started by /compact, whose reply is a summary
	// rather than an answer. preamble is what that summary becomes: text put in
	// front of the next message, which is how a new backend session is told
	// where the old one had got to.
	compacting bool
	preamble   string

	// history is what you have asked this agent, oldest first, for ↑ in the
	// input box. Kept apart from the conversation so /clear drops what the
	// agent knows without dropping what you typed.
	history []string

	// cliCmds is every command the backend said it has, from the opening
	// report of its first run. Authoritative where the filesystem walk is a
	// guess: it covers the CLI's own built-ins too, which are not on disk.
	cliCmds []string

	cmds        []agent.Command // slash commands and skills, loaded on first use
	cmdsLoaded  bool
	files       []string // project files for @-mentions, loaded on first use
	filesLoaded bool
}

// maxProjectFiles bounds what the @-file list will hold. A repo bigger than
// this is one where the first few characters of a name narrow it down anyway.
const maxProjectFiles = 5000

// queuedTurn is a message waiting for the current one to finish. It carries
// both forms for the same reason startTurn takes both: what the conversation
// shows and what the agent is given differ when an image was pasted. The files
// behind those [Image #n] markers ride along too, so taking the message back
// out of the queue brings its pictures with it.
type queuedTurn struct {
	shown, prompt string
	images        []string
}

// enqueue puts a message behind the running turn.
func (s *Session) enqueue(shown, prompt string, images []string) int {
	s.queued = append(s.queued, queuedTurn{shown: shown, prompt: prompt, images: images})
	return len(s.queued)
}

// nextQueued takes the message that has been waiting longest.
func (s *Session) nextQueued() (queuedTurn, bool) {
	if len(s.queued) == 0 {
		return queuedTurn{}, false
	}
	q := s.queued[0]
	s.queued = s.queued[1:]
	return q, true
}

// dropQueue forgets everything waiting and says how much that was.
func (s *Session) dropQueue() int {
	n := len(s.queued)
	s.queued = nil
	return n
}

// Queued is what is still waiting, for the box above the input.
func (s *Session) Queued() []string {
	out := make([]string, len(s.queued))
	for i, q := range s.queued {
		out[i] = q.shown
	}
	return out
}

// reset drops the conversation and the backend session behind it, which is
// what /clear means: the next message starts a run with no --resume, so the
// agent has never heard of any of this. Spend is not reset, because it was
// spent; the permission mode and model are the user's settings, not history.
func (s *Session) reset() {
	s.closeConv()
	s.agentSID = ""
	s.ctxTokens, s.ctxWindow = 0, 0
	s.preamble = ""
	s.dropQueue()
	s.tl.Restore(nil)
	s.introduce()
}

// usageRefresh is how often the allowance is re-read while crema is just
// sitting there. The status-line bridge rewrites it every time an interactive
// session redraws, which is often; once a turn would be too rare to watch a
// bar move, and every frame would be silly. It matches the idle heartbeat,
// so each wake-up reads fresh numbers rather than deciding they can wait.
const usageRefresh = heartbeatEvery

// maybeRefreshLimits re-reads the allowance if it has been a while. Called
// from the frame, so the bars follow the file without waiting for a turn.
func (s *Session) maybeRefreshLimits() {
	if time.Since(s.limitsAt) >= usageRefresh {
		s.refreshLimits(nil)
	}
}

// Commands lists the backend's slash commands and skills for this working
// directory. Discovery walks the filesystem, so the answer is kept until
// Reload drops it.
func (s *Session) Commands() []agent.Command {
	if !s.cmdsLoaded {
		s.cmds, s.cmdsLoaded = s.Backend.Commands(s.Dir), true
	}
	return s.cmds
}

// Files lists the project's files for @-mentions, cached the same way.
func (s *Session) Files() []string {
	if !s.filesLoaded {
		s.files, s.filesLoaded = gitdiff.ListFiles(s.Dir, maxProjectFiles), true
	}
	return s.files
}

// Reload forgets both cached lists, so a command or file added while crema is
// running shows up on the next lookup.
func (s *Session) Reload() {
	s.cmds, s.cmdsLoaded = nil, false
	s.files, s.filesLoaded = nil, false
	s.refreshLimits(nil)
}

// defaultMode is what a new agent starts with: full access, if the backend has
// it. Crema drives these CLIs headlessly, where there is no prompt to approve
// anything at — a tool that would ask instead fails the turn. Under anything
// narrower the agent reads as broken rather than as restrained: it announces a
// plan, runs one command, and stops on an error nobody can answer. The looser
// default states the bargain plainly instead, and every agent's banner says
// which mode it is in. Narrow it per agent with the permissions button, or for
// the first agent with --permission-mode.
func defaultMode(backend agent.Agent) agent.PermissionMode {
	if backend == nil {
		return agent.PermissionDefault
	}
	want := []agent.PermissionMode{agent.PermissionFull, agent.PermissionAcceptEdits}
	for _, w := range want {
		for _, has := range backend.Modes() {
			if has == w {
				return w
			}
		}
	}
	return agent.PermissionDefault
}

func NewSession(id int, backend agent.Agent, dir string) *Session {
	s := &Session{
		ID:          id,
		Backend:     backend,
		Dir:         dir,
		Permission:  defaultMode(backend),
		Model:       agent.DefaultModel,
		AutoCompact: true,
		tl:          NewTimeline(80, 20),
		dp:          NewDiffPanel(40, 20),
	}
	// The allowance is a fact about the account, not about a turn: worth
	// showing before the first message rather than only after it.
	s.refreshLimits(nil)
	return s
}

// introduce writes the opening banner. Restored sessions skip it — they replay
// the banner they were saved with instead of stacking a second one.
func (s *Session) introduce() *Session {
	s.tl.Append(Block{Kind: BlockSystem, Text: s.Backend.Label() + " · " + s.modeNote() +
		"\nworking in " + s.Dir +
		"\ntype / for commands and skills  ·  @ for files  ·  press ↓ for the model or permissions"})
	return s
}

// modeNote states the active permission mode and what it means in practice.
func (s *Session) modeNote() string {
	return "permissions: " + s.Permission.Label() + " — " + s.Permission.Describe()
}

// SetPermission changes the mode and records it in the conversation, so the
// timeline always explains why a later turn could or couldn't run a command.
func (s *Session) SetPermission(p agent.PermissionMode) {
	if s.Permission == p {
		return
	}
	s.Permission = p
	s.closeConv() // the mode is a command-line flag: the next turn needs a new process
	s.tl.Append(Block{Kind: BlockSystem, Text: s.modeNote()})
}

// SetModel changes the model for the next turn.
func (s *Session) SetModel(m string) {
	if s.Model == m {
		return
	}
	s.Model = m
	s.closeConv() // likewise the model
	name := m
	if m == agent.DefaultModel {
		name = "the CLI's default"
	}
	s.tl.Append(Block{Kind: BlockSystem, Text: "model: " + name + " (applies to the next message)"})
}

// Title is the sidebar label: the name you gave this agent, or failing that
// the backend and the directory's last element. Two agents on the same project
// are told apart by what they are for, which only you know.
func (s *Session) Title() string {
	if s.Name != "" {
		return s.Name
	}
	return s.derivedTitle()
}

func (s *Session) derivedTitle() string {
	base := filepath.Base(s.Dir)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = s.Dir
	}
	return s.Backend.Name() + " · " + base
}

// Rename gives the agent a name of its own, or takes it back to the derived
// one when given nothing.
func (s *Session) Rename(name string) {
	s.Name = strings.TrimSpace(name)
}

func (s *Session) Busy() bool { return s.busy }

// Elapsed is how long the current turn has been running (0 when idle).
func (s *Session) Elapsed() time.Duration {
	if !s.busy {
		return 0
	}
	return time.Since(s.turnStart)
}

func (s *Session) SetSize(timelineW, diffW, paneH int) {
	s.tl.SetSize(timelineW, paneH)
	if diffW > 0 {
		s.dp.SetSize(diffW-2, paneH-2) // inside the rounded border
	}
}

// startTurn spawns the backend. Returns nil when the agent is unavailable,
// having already reported the reason into the timeline.
//
// shown is what goes in the conversation and prompt is what the agent is
// given. They differ when the draft carried a pasted image: the transcript
// keeps the [Image #1] that was on screen, while the agent is handed the file
// to read.
func (s *Session) startTurn(shown, prompt string) tea.Cmd {
	if err := s.Backend.Available(); err != nil {
		s.tl.Append(Block{Kind: BlockError, Text: err.Error()})
		return nil
	}
	s.tl.Append(Block{Kind: BlockUser, Text: shown})
	s.tl.GotoEnd() // sending is done reading: show the message and what answers it
	if s.preamble != "" {
		// A compacted session opens with the summary of the one before it.
		prompt, s.preamble = s.preamble+"\n\n---\n\n"+prompt, ""
	}
	s.busy = true
	s.turnStart = time.Now()
	s.activity, s.turnOut, s.turnEvents = "working", 0, 0
	s.canceled = false
	s.lastOpts = agent.RunOptions{
		Prompt: prompt, Dir: s.Dir, SessionID: s.agentSID,
		Permission: s.Permission, Model: s.Model,
	}
	if _, ok := s.Backend.(agent.Streamer); ok {
		return s.sendToConv(prompt)
	}
	s.streamSeq++
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	return startStream(s.Backend, ctx, s.lastOpts, s.ID, s.streamSeq)
}

// noteResult absorbs one result line without ending the turn. One run can
// produce several: a turn that launched an async subagent "ends", then the
// task's completion revives it for another leg with its own result. The
// stream closing — the process exiting — is what really ends a turn; acting
// on the first result used to double-count money and fire queued messages
// into a turn that was still going.
func (s *Session) noteResult(r *agent.TurnResult) {
	if r == nil {
		return
	}
	s.lastResult = r
	if r.SessionID != "" {
		s.agentSID = r.SessionID
	}
	s.cost += r.CostUSD // adapters report each leg's growth, so adding is safe
	if r.ContextWindow > 0 {
		s.ctxTokens, s.ctxWindow = r.ContextTokens, r.ContextWindow
	}
	if len(r.RateLimits) > 0 {
		// The turn's own report is as fresh as usage data gets; write it
		// where the status-line bridge writes, so every agent and the next
		// run see it too.
		_ = agent.RecordUsage(r.RateLimits)
		s.refreshLimits(r.RateLimits)
	} else if r.RateLimit != nil {
		s.refreshLimits([]agent.RateLimit{*r.RateLimit})
	} else {
		s.refreshLimits(nil)
	}
}

// endTurn runs when the stream closes.
func (s *Session) endTurn() {
	r := s.lastResult
	s.lastResult = nil
	// A turn that ends cleanly having produced nothing at all would otherwise
	// render as pure silence — the message went, the CLI came back "fine",
	// and the conversation shows no answer of any kind. Measured in the wild:
	// resuming a session whose last run died mid-tool-call gave a 1.6-second
	// "success" with zero tokens either way. Whatever the cause, silence is
	// the one thing crema must not show.
	if r != nil && !r.Canceled && r.Err == "" && s.turnEvents == 0 {
		s.tl.Append(Block{Kind: BlockError, Text: fmt.Sprintf(
			"the CLI ended this turn after %.1fs having produced nothing — no reply, no error. "+
				"That usually means the saved session could not be picked up (a previous run "+
				"stopping mid-tool-call can do it). Sending the message again often works; "+
				"/clear starts the agent fresh if it keeps happening.",
			float64(r.DurationMS)/1000)})
	}
	s.busy = false
	if s.conv == nil {
		s.stream = nil // a held-open conversation keeps listening between turns
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	// A background task belongs to the run that launched it. If the CLI
	// exited without ever saying how one ended, it is not still going —
	// nothing is left to run it — so it stops being counted as running
	// rather than haunting the sidebar with work that finished unseen.
	for i := range s.tasks {
		if s.tasks[i].Status == "running" {
			s.tasks[i].Status = "ended with the turn"
		}
	}
}

// noteTask merges one background-task report over what is already known. The
// reports are sparse — a progress line has tokens but no output file, the
// notification has the file but no tokens — so newer non-empty fields win and
// the rest stays.
func (s *Session) noteTask(u *agent.TaskUpdate) {
	if u == nil || u.ID == "" {
		return
	}
	for i := range s.tasks {
		if s.tasks[i].ID != u.ID {
			continue
		}
		t := &s.tasks[i]
		if u.Status != "" {
			t.Status = u.Status
		}
		if u.Desc != "" {
			t.Desc = u.Desc
		}
		if u.Type != "" {
			t.Type = u.Type
		}
		if u.LastTool != "" {
			t.LastTool = u.LastTool
		}
		if u.Tokens > 0 {
			t.Tokens = u.Tokens
		}
		if u.ToolUses > 0 {
			t.ToolUses = u.ToolUses
		}
		if u.OutputFile != "" {
			t.OutputFile = u.OutputFile
		}
		if u.Summary != "" {
			t.Summary = u.Summary
		}
		return
	}
	s.tasks = append(s.tasks, *u)
}

// Background is what is running behind the turn, split the way the CLI's own
// status line splits it: shells it backgrounded, and subagents it handed work
// to. They read differently — a shell is a command you could go and watch, a
// subagent is an agent of its own — so they are counted apart.
func (s *Session) Background() (shells, agents int) {
	for _, t := range s.tasks {
		if t.Status != "running" {
			continue
		}
		if t.Type == "local_bash" {
			shells++
		} else {
			agents++
		}
	}
	return shells, agents
}

// RunningTasks is how much background work is still going.
func (s *Session) RunningTasks() int {
	n := 0
	for _, t := range s.tasks {
		if t.Status == "running" {
			n++
		}
	}
	return n
}

// refreshLimits works out what to say about the subscription's allowance. The
// backend's usage cache is preferred — it is the only source with a
// percentage — and the newest rate_limit_event fills in for a window the cache
// doesn't cover, contributing its reset time and no false precision.
func (s *Session) refreshLimits(fromTurn []agent.RateLimit) {
	s.limitsAt = time.Now()
	if len(fromTurn) > 0 {
		s.turnLimits = fromTurn
	}
	var limits []agent.RateLimit
	if u, ok := s.Backend.(agent.UsageReporter); ok {
		limits = u.Usage() // the recorded file already drops the expired
	}
	now := time.Now()
	for _, l := range s.turnLimits {
		if !l.ResetsAt.IsZero() && !l.ResetsAt.After(now) {
			continue // that window has reset; whatever it said no longer holds
		}
		covered := false
		for _, have := range limits {
			covered = covered || have.Label() == l.Label()
		}
		if !covered {
			limits = append(limits, l)
		}
	}
	s.limits = limits
}

// persistent reports whether this agent holds its CLI process open between
// turns. It is what decides when a turn is over: a process-per-turn backend
// ends its turn by exiting, a held-open one by saying so.
func (s *Session) persistent() bool {
	_, ok := s.Backend.(agent.Streamer)
	return ok
}

// sendToConv hands the prompt to the agent's open process, opening one first
// if there isn't a live one. The listener it returns runs for the whole
// conversation rather than the turn, so anything the CLI says between turns
// — a background task finishing, say — still arrives.
func (s *Session) sendToConv(prompt string) tea.Cmd {
	fresh := false
	if s.conv == nil {
		st, ok := s.Backend.(agent.Streamer)
		if !ok {
			return nil
		}
		ctx, stop := context.WithCancel(context.Background())
		conv, err := st.Open(ctx, s.lastOpts)
		if err != nil {
			stop()
			s.tl.Append(Block{Kind: BlockError, Text: err.Error()})
			s.busy = false
			return nil
		}
		s.conv, s.convStop, fresh = conv, stop, true
		s.streamSeq++
		s.stream = conv.Events()
	}
	if err := s.conv.Send(prompt); err != nil {
		// The process died between turns; the next message opens a new one.
		s.closeConv()
		s.tl.Append(Block{Kind: BlockError, Text: "the agent's process ended — send again to start a new one: " + err.Error()})
		s.busy = false
		return nil
	}
	s.convAt = time.Now()
	if !fresh {
		return nil // the listener from the first turn is still running
	}
	return waitForEvent(s.stream, s.ID, s.streamSeq)
}

// closeConv ends the agent's process. The conversation itself is not lost —
// the CLI keeps the transcript, and the next turn resumes it by id — so this
// is what every setting that lives on the command line does: model,
// permissions, and starting over.
func (s *Session) closeConv() {
	if s.conv != nil {
		go s.conv.Close() // draining its channel can block; never on the UI
		s.conv = nil
	}
	if s.convStop != nil {
		s.convStop()
		s.convStop = nil
	}
	s.stream = nil
}

// maybeCloseIdleConv lets go of a process that has been sitting longer than
// the prompt cache it exists to keep warm. Past that the process is only
// holding memory.
func (s *Session) maybeCloseIdleConv() {
	if s.conv == nil || s.busy || s.convAt.IsZero() {
		return
	}
	if time.Since(s.convAt) > convIdleLife {
		s.closeConv()
	}
}

// convIdleLife matches the one-hour prompt cache: a held process whose cache
// has expired is worth no more than a new one.
const convIdleLife = time.Hour

// cancelTurn stops an in-flight turn. A process-per-turn backend is killed by
// its context; a held-open conversation is closed, since the CLI has no way
// to be told "stop that" over stdin — and the next message opens a new
// process on the same conversation.
func (s *Session) cancelTurn() bool {
	if !s.busy {
		return false
	}
	if s.conv != nil {
		s.canceled = true
		s.closeConv()
		s.tl.Append(Block{Kind: BlockSystem, Text: "canceling the current turn…"})
		return true
	}
	if s.cancel == nil {
		return false
	}
	s.cancel()
	s.tl.Append(Block{Kind: BlockSystem, Text: "canceling the current turn…"})
	return true
}

// close cancels any running turn and drains the stream so its goroutine ends.
func (s *Session) close() {
	s.closeConv()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.stream != nil {
		go drain(s.stream)
		s.stream = nil
	}
	s.busy = false
}

func (s *Session) collectDiff() tea.Cmd {
	s.diffSeq++
	seq, dir, id := s.diffSeq, s.Dir, s.ID
	return func() tea.Msg { return diffMsg{sess: id, seq: seq, ds: gitdiff.Collect(dir)} }
}

// scheduleDiff debounces refreshes: only the newest schedule survives, so a
// burst of edits costs one `git diff` instead of one per file.
func (s *Session) scheduleDiff() tea.Cmd {
	s.diffSeq++
	seq, id := s.diffSeq, s.ID
	return tea.Tick(diffDebounce, func(time.Time) tea.Msg {
		return diffTickMsg{sess: id, seq: seq}
	})
}
