package ui

import (
	"context"
	"path/filepath"
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
	// saved between runs.
	Permission agent.PermissionMode
	Model      string

	tl *Timeline
	dp *DiffPanel

	busy      bool
	turnStart time.Time
	stream    <-chan agent.Event
	streamSeq int
	cancel    context.CancelFunc
	agentSID  string // backend session id, for resume
	cost      float64
	ctxTokens int64 // context occupancy reported by the last turn
	ctxWindow int64
	limit     *agent.RateLimit
	lastOpts  agent.RunOptions

	diff        gitdiff.DiffSet
	diffSeq     int
	diffApplied int
}

func NewSession(id int, backend agent.Agent, dir string) *Session {
	return &Session{
		ID:      id,
		Backend: backend,
		Dir:     dir,
		// acceptEdits is the historical crema default: it lets the agent write
		// files, which is the point, without handing it the shell.
		Permission: agent.PermissionAcceptEdits,
		Model:      agent.DefaultModel,
		tl:         NewTimeline(80, 20),
		dp:         NewDiffPanel(40, 20),
	}
}

// introduce writes the opening banner. Restored sessions skip it — they replay
// the banner they were saved with instead of stacking a second one.
func (s *Session) introduce() *Session {
	s.tl.Append(Block{Kind: BlockSystem, Text: s.Backend.Label() + " · " + s.modeNote() +
		"\nworking in " + s.Dir + "  ·  ctrl+p for permissions and model"})
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
	s.tl.Append(Block{Kind: BlockSystem, Text: s.modeNote()})
}

// SetModel changes the model for the next turn.
func (s *Session) SetModel(m string) {
	if s.Model == m {
		return
	}
	s.Model = m
	name := m
	if m == agent.DefaultModel {
		name = "the CLI's default"
	}
	s.tl.Append(Block{Kind: BlockSystem, Text: "model: " + name + " (applies to the next message)"})
}

// Title is the sidebar label: backend plus the directory's last element.
func (s *Session) Title() string {
	base := filepath.Base(s.Dir)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = s.Dir
	}
	return s.Backend.Name() + " · " + base
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
func (s *Session) startTurn(prompt string) tea.Cmd {
	if err := s.Backend.Available(); err != nil {
		s.tl.Append(Block{Kind: BlockError, Text: err.Error()})
		return nil
	}
	s.tl.Append(Block{Kind: BlockUser, Text: prompt})
	s.streamSeq++
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.busy = true
	s.turnStart = time.Now()
	s.lastOpts = agent.RunOptions{
		Prompt: prompt, Dir: s.Dir, SessionID: s.agentSID,
		Permission: s.Permission, Model: s.Model,
	}
	return startStream(s.Backend, ctx, s.lastOpts, s.ID, s.streamSeq)
}

func (s *Session) endTurn(r *agent.TurnResult) {
	s.busy = false
	s.stream = nil
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if r != nil {
		if r.SessionID != "" {
			s.agentSID = r.SessionID
		}
		s.cost += r.CostUSD
		if r.ContextWindow > 0 {
			s.ctxTokens, s.ctxWindow = r.ContextTokens, r.ContextWindow
		}
		if r.RateLimit != nil {
			s.limit = r.RateLimit
		}
	}
}

// cancelTurn stops an in-flight turn; the adapter still delivers a final
// canceled TurnEnd, which is what flips busy back off.
func (s *Session) cancelTurn() bool {
	if !s.busy || s.cancel == nil {
		return false
	}
	s.cancel()
	s.tl.Append(Block{Kind: BlockSystem, Text: "canceling the current turn…"})
	return true
}

// close cancels any running turn and drains the stream so its goroutine ends.
func (s *Session) close() {
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
