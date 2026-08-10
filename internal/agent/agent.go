// Package agent normalizes the official coding-agent CLIs (Claude Code, Codex)
// into a single event stream. Authentication belongs entirely to those CLIs —
// crema never reads, stores, or passes an API key.
package agent

import "context"

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

type TurnResult struct {
	SessionID    string // pass back via RunOptions.SessionID to resume
	DurationMS   int64
	CostUSD      float64 // 0 when the backend doesn't report USD (codex)
	InputTokens  int64
	OutputTokens int64
	Canceled     bool
	Err          string // non-empty ⇒ the turn failed
}

type RunOptions struct {
	Prompt    string
	Dir       string // working directory for the agent subprocess
	SessionID string // "" = new session; else resume
}

type Agent interface {
	Name() string  // stable id: "claude" | "codex" | "mock"
	Label() string // display: "Claude Code" | "Codex" | "Mock"
	Available() error
	Run(ctx context.Context, opts RunOptions) (<-chan Event, error)
}
