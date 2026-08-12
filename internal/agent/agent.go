// Package agent normalizes the official coding-agent CLIs (Claude Code, Codex)
// into a single event stream. Authentication belongs entirely to those CLIs —
// crema never reads, stores, or passes an API key.
package agent

import (
	"context"
	"strings"
	"time"
)

type Kind int

const (
	KindText       Kind = iota // assistant prose (Thinking=true → reasoning channel)
	KindToolCall               //
	KindToolOutput             //
	KindTurnEnd                // ALWAYS the final event before the channel closes
	KindError                  // non-fatal error surfaced mid-stream
)

type Event struct {
	Kind     Kind
	Text     string      // KindText, KindError
	Thinking bool        // KindText only
	Tool     *ToolCall   // KindToolCall
	Output   *ToolOutput // KindToolOutput
	Result   *TurnResult // KindTurnEnd
}

type ToolCall struct {
	ID    string // backend's tool/item id; matches ToolOutput.ToolID
	Name  string // "Bash", "shell", "apply_patch", …
	Input string // human-readable, pretty-printed; NEVER truncated here
}

type ToolOutput struct {
	ToolID  string
	Content string
	IsError bool
}

// RateLimit is the usage window the backend reports, e.g. Claude Code's
// rolling five-hour and seven-day allowances.
type RateLimit struct {
	Type        string    // backend's window id, e.g. "five_hour"
	Utilization float64   // 0..1 of the window consumed
	ResetsAt    time.Time // zero when the backend didn't say
}

// Label shortens the backend's window id for the status bar.
func (r RateLimit) Label() string {
	switch {
	case strings.Contains(r.Type, "five_hour"):
		return "5h"
	case strings.Contains(r.Type, "seven_day"):
		return "7d"
	case r.Type == "":
		return "limit"
	default:
		return r.Type
	}
}

type TurnResult struct {
	SessionID    string // pass back via RunOptions.SessionID to resume
	DurationMS   int64
	CostUSD      float64 // 0 when the backend doesn't report USD (codex)
	InputTokens  int64
	OutputTokens int64
	// ContextTokens is how much of the window the conversation now occupies
	// (fresh input + cache reads + cache writes); ContextWindow is the model's
	// capacity. Both 0 when the backend doesn't report them.
	ContextTokens int64
	ContextWindow int64
	RateLimit     *RateLimit
	Canceled      bool
	Err           string // non-empty ⇒ the turn failed
}

// PermissionMode is how much the agent may do without asking. Headless CLIs
// cannot display an approval prompt, so anything that would ask instead fails
// the tool — which is why the permissive modes exist at all.
type PermissionMode string

const (
	// PermissionDefault leaves the CLI's own default in place. Safest, but in
	// headless mode most tools that need approval simply fail.
	PermissionDefault PermissionMode = "default"
	// PermissionAcceptEdits auto-approves file edits only. Shell commands and
	// reads outside the project still require approval, so they still fail.
	PermissionAcceptEdits PermissionMode = "acceptEdits"
	// PermissionFull approves everything. The agent can run any command.
	PermissionFull PermissionMode = "full"
	// PermissionPlan is read-only: the agent plans but changes nothing.
	PermissionPlan PermissionMode = "plan"
)

// Label is the short name shown in the UI.
func (p PermissionMode) Label() string {
	switch p {
	case PermissionAcceptEdits:
		return "edits"
	case PermissionFull:
		return "full access"
	case PermissionPlan:
		return "plan only"
	default:
		return "ask"
	}
}

// Describe explains the practical consequence, including the headless caveat.
func (p PermissionMode) Describe() string {
	switch p {
	case PermissionAcceptEdits:
		return "file edits apply; shell commands are still blocked"
	case PermissionFull:
		return "no approval prompts; the agent can run any command"
	case PermissionPlan:
		return "read-only; the agent plans but changes nothing"
	default:
		return "the CLI's own default; most tools needing approval will fail"
	}
}

// DefaultModel means "whatever the CLI is already configured to use".
const DefaultModel = ""

type RunOptions struct {
	Prompt     string
	Dir        string // working directory for the agent subprocess
	SessionID  string // "" = new session; else resume
	Permission PermissionMode
	Model      string // "" = the CLI's configured default
}

type Agent interface {
	Name() string  // stable id: "claude" | "codex" | "mock"
	Label() string // display: "Claude Code" | "Codex" | "Mock"
	Available() error
	// Modes lists the permission modes this backend supports, least permissive
	// first. Crema only ever offers the user a mode the backend can honor.
	Modes() []PermissionMode
	// Models lists selectable model aliases. DefaultModel is always first.
	Models() []string
	Run(ctx context.Context, opts RunOptions) (<-chan Event, error)
}
