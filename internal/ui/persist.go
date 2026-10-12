package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
)

const (
	stateVersion = 1
	// Caps keep the state file small enough to rewrite after every turn. Both
	// are applied with a visible marker in the restored conversation — crema
	// does not drop history silently any more than it folds output silently.
	maxSavedBlocks     = 300
	maxSavedBlockRunes = 20000
)

// SavedSession is one agent as it is written to disk.
type SavedSession struct {
	Backend  string  `json:"backend"`
	Dir      string  `json:"dir"`
	AgentSID string  `json:"agent_session_id,omitempty"`
	Cost     float64 `json:"cost,omitempty"`
	// How full the model's context window was. Saved with the session id it
	// belongs to: a resumed conversation is still that many tokens long, and
	// the status bar would otherwise say it knew nothing until the next turn
	// ended, next to a spend that had carried over.
	ContextTokens int64                `json:"context_tokens,omitempty"`
	ContextWindow int64                `json:"context_window,omitempty"`
	Permission    agent.PermissionMode `json:"permission,omitempty"`
	Model         string               `json:"model,omitempty"`
	// Effort is how hard this agent is asked to think. Empty — including in a
	// state file written before the setting existed — is the CLI's own default.
	Effort string `json:"effort,omitempty"`
	Name   string `json:"name,omitempty"`
	// History is what you typed at this agent, for ↑ in the input box. Saved
	// apart from the conversation because it outlives it: /clear drops what
	// the agent knows, not what you asked.
	History []string `json:"history,omitempty"`
	// CLICommands is what the backend last reported it has, kept so the /
	// list is complete from the first keystroke of the next run rather than
	// only after a turn.
	CLICommands []string `json:"cli_commands,omitempty"`
	Blocks      []Block  `json:"blocks,omitempty"`
}

// State is everything crema remembers between runs.
type State struct {
	Version  int            `json:"version"`
	SavedAt  time.Time      `json:"saved_at"`
	Theme    string         `json:"theme,omitempty"`
	Active   int            `json:"active"`
	Sessions []SavedSession `json:"sessions"`
}

// statePathOverride lets tests keep out of the real config directory.
var statePathOverride string

// StatePath is the file crema remembers open agents in.
func StatePath() string {
	if statePathOverride != "" {
		return statePathOverride
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "crema", "state.json")
}

// LoadState reads saved agents. Any problem — missing file, corrupt JSON, a
// version crema no longer understands — yields an empty state rather than an
// error, because failing to remember must never stop the app from starting.
func LoadState() State {
	FlushState() // what has been saved should be what is read
	p := StatePath()
	if p == "" {
		return State{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return State{}
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil || st.Version != stateVersion {
		return State{}
	}
	return st
}

// SaveState writes atomically, so a crash mid-write cannot leave a truncated
// file that would lose every remembered agent.
//
// It waits for any save already queued in the background, so a slower write of
// an older state can never land after this one and roll the file backwards.
func SaveState(st State) error {
	FlushState()
	return writeStateTo(StatePath(), st)
}

// writeStateTo is the write itself, for a path decided by the caller.
func writeStateTo(p string, st State) error {
	if p == "" {
		return errors.New("no user config directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	// 0600: conversations and tool output can contain anything from the repo.
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// StateSnapshot captures the open agents for saving.
//
// The copy it returns is handed to another goroutine to write, so nothing in
// it may be something the interface goes on changing in place. Blocks are
// copied by trimForSaving and their strings never change; history is copied
// here, because once it is full remember shifts it along inside the same
// array, and a writer reading the shared array mid-shift would save a garbled
// list.
func (a *App) StateSnapshot() State {
	st := State{
		Version: stateVersion,
		SavedAt: time.Now(),
		Theme:   CurrentMode().String(),
		Active:  a.active,
	}
	for _, s := range a.sessions {
		st.Sessions = append(st.Sessions, SavedSession{
			Backend:       s.Backend.Name(),
			Dir:           s.Dir,
			AgentSID:      s.agentSID,
			Cost:          s.cost,
			ContextTokens: s.ctxTokens,
			ContextWindow: s.ctxWindow,
			Permission:    s.Permission,
			Model:         s.Model,
			Effort:        s.Effort,
			Name:          s.Name,
			History:       append([]string(nil), s.history...), // its own copy; see the doc above
			CLICommands:   s.cliCmds,
			Blocks:        trimForSaving(s.tl.Blocks()),
		})
	}
	return st
}

// persist writes the current agents, surfacing any failure in the status bar
// rather than silently forgetting them.
// persist saves the open agents. The copy is taken here, where the state is
// consistent; the writing happens on a goroutine of its own. See saver.go.
func (a *App) persist() {
	saveLater(a.StateSnapshot())
}

// trimForSaving bounds the state file, marking whatever it drops.
func trimForSaving(bs []Block) []Block {
	if len(bs) > maxSavedBlocks {
		dropped := len(bs) - maxSavedBlocks
		bs = append([]Block{{
			Kind: BlockSystem,
			Text: fmt.Sprintf("… %d earlier entries were not saved (crema keeps the last %d)",
				dropped, maxSavedBlocks),
		}}, bs[dropped:]...)
	}
	out := make([]Block, len(bs))
	for i, b := range bs {
		// A string is never more runes than it is bytes, so a short one cannot
		// be over the cap and is not decoded to find out. This runs on the
		// interface's own goroutine, over every block of every agent, on every
		// save — decoding all of it each time was a real share of the cost.
		if len(b.Text) <= maxSavedBlockRunes {
			out[i] = b
			continue
		}
		if r := []rune(b.Text); len(r) > maxSavedBlockRunes {
			b.Text = string(r[:maxSavedBlockRunes]) +
				fmt.Sprintf("\n… %d characters were not saved (crema cap %d)",
					len(r)-maxSavedBlockRunes, maxSavedBlockRunes)
		}
		out[i] = b
	}
	return out
}

// RestoreSessions rebuilds saved agents onto a. It returns the number restored
// and a note about any that could not be, so the user learns why an agent they
// expected is missing instead of just not seeing it.
func (a *App) RestoreSessions(st State) (int, []string) {
	var skipped []string
	for _, ss := range st.Sessions {
		backend := findBackend(a.reg, ss.Backend)
		if backend == nil {
			skipped = append(skipped, fmt.Sprintf("%s — no %q agent in this build", ss.Dir, ss.Backend))
			continue
		}
		if fi, err := os.Stat(ss.Dir); err != nil || !fi.IsDir() {
			skipped = append(skipped, fmt.Sprintf("%s — directory is gone", ss.Dir))
			continue
		}
		s := a.addSession(backend, ss.Dir)
		s.agentSID = ss.AgentSID
		s.cost = ss.Cost
		s.Model = ss.Model
		if backendOffersEffort(backend, ss.Effort) {
			s.Effort = ss.Effort
		}
		s.Name = ss.Name
		s.history = ss.History
		s.cliCmds = ss.CLICommands
		if ss.AgentSID != "" {
			// Only alongside a session that will actually be resumed — without
			// one the next turn starts empty, whatever the last one measured.
			s.ctxTokens, s.ctxWindow = ss.ContextTokens, ss.ContextWindow
		}
		if ss.Permission != "" && backendSupports(backend, ss.Permission) {
			s.Permission = ss.Permission
		}
		s.tl.Restore(ss.Blocks)
		s.tl.Append(Block{Kind: BlockSystem, Text: resumedNote(ss, st.SavedAt)})
	}
	if n := len(a.sessions); n > 0 && st.Active >= 0 && st.Active < n {
		a.active = st.Active
	}
	return len(a.sessions), skipped
}

// backendOffersEffort keeps a saved level from being handed to a backend that
// has no such setting, or that has stopped offering that level — the same
// guard the permission mode gets, for the same reason.
func backendOffersEffort(backend agent.Agent, level string) bool {
	if level == agent.DefaultEffort {
		return false
	}
	e, ok := backend.(agent.EffortChooser)
	if !ok {
		return false
	}
	for _, l := range e.Efforts() {
		if l == level {
			return true
		}
	}
	return false
}

func resumedNote(ss SavedSession, savedAt time.Time) string {
	when := "a previous run"
	if !savedAt.IsZero() {
		when = savedAt.Format("2006-01-02 15:04")
	}
	if ss.AgentSID == "" {
		return "restored from " + when + " · no agent session to resume, the next message starts fresh"
	}
	return "restored from " + when + " · continuing agent session " + ss.AgentSID
}

// backendSupports guards restore: a mode saved under an older build (or a
// different backend) must not be silently handed to one that can't honor it.
func backendSupports(b agent.Agent, p agent.PermissionMode) bool {
	for _, m := range b.Modes() {
		if m == p {
			return true
		}
	}
	return false
}

func findBackend(reg *agent.Registry, name string) agent.Agent {
	for _, b := range reg.Agents {
		if b.Name() == name {
			return b
		}
	}
	return nil
}
