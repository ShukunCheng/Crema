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
	KindTurnEnd                // ends a leg of the turn; the last one precedes the close
	KindError                  // non-fatal error surfaced mid-stream
	KindReady                  // the backend's opening report about itself
	KindTask                   // a background task's lifecycle: subagents and backgrounded commands
	KindControl                // the CLI's answer to a control request sent down its stdin
	KindTaskList               // the CLI's whole list of backgrounded commands; one it dropped has ended
	KindUser                   // something said to the agent; only a subagent transcript produces these
	KindRate                   // the account's allowance, the moment the CLI reports it
)

type Event struct {
	Kind     Kind
	Text     string      // KindText, KindError, KindUser
	Thinking bool        // KindText only
	Tool     *ToolCall   // KindToolCall
	Output   *ToolOutput // KindToolOutput
	Result   *TurnResult // KindTurnEnd
	// OutTokens is how much the model has written this turn so far, running
	// total, when the backend says as it goes. 0 means it hasn't said — which
	// is every event from a backend that only reports at the end.
	OutTokens int64
	// Commands is every slash command this backend has, by its own account.
	// KindReady only.
	Commands []string
	// SubID names the tool call whose subagent produced this event — the
	// parent Task/Agent id the CLI tags nested work with. Empty for the main
	// conversation.
	SubID string
	// Task is a background task's lifecycle update. KindTask only.
	Task *TaskUpdate
	// Control is the CLI's reply to a control request. KindControl only.
	Control *ControlReply
	// Tasks is the CLI's current list of backgrounded commands. KindTaskList
	// only. Measured (2.1.250): sent when one starts and when the list
	// empties, listing task_id, task_type and description.
	Tasks []TaskUpdate
	// Rates is the allowance as the CLI just reported it: every window it
	// named, soonest reset first. KindRate only.
	Rates []RateLimit
}

// TaskUpdate is one report about a background task — a subagent the model
// launched, or a command it backgrounded. The CLI sends these as it goes:
// started, progress heartbeats, and a completion that names the file the
// task's full output went to. Fields are sparse; each update carries what
// that report knew.
type TaskUpdate struct {
	ID        string // the CLI's own task id
	ToolUseID string // the tool call that launched it
	Type      string // "general-purpose", "local_bash", …
	Desc      string
	Status    string // running | completed | failed | stopped
	// Why is crema's own account of how it ended, when crema is the one who
	// knows: a subagent does not stop on its own when the process it lives in
	// is closed, and the reason that process closed is the reason it stopped.
	Why        string
	LastTool   string // what the subagent is running right now
	Tokens     int64  // the subagent's own spend so far
	ToolUses   int
	OutputFile string // where the CLI wrote the task's full output
	Summary    string // the task's own closing words
	// Started is when crema first heard of it. The CLI does not say when a
	// task began, and the first word about one is close enough to count from.
	Started time.Time
	// Ended is when it stopped running, set by whoever noticed. A row that
	// has just failed is kept on screen for a moment afterwards, the way the
	// CLI's own subagent panel keeps one.
	Ended time.Time
	// SpawnDepth is how far down the tree it is: 1 for a subagent the main
	// agent launched, 2 for one that subagent launched. The CLI sends it on
	// task_started and caps the tree at three.
	SpawnDepth int
	// Backgrounded is whether the task runs behind the turn rather than
	// holding it. The CLI's background_tasks_changed list is the truth about
	// those — a backgrounded task it no longer lists has ended — and says
	// nothing about a foreground subagent, which is never on it.
	Backgrounded bool
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
	Type string // backend's window id, e.g. "five_hour"
	// Utilization is 0..1 of the window consumed, and only means anything when
	// Known is set. Claude Code 2.1.229 reports the window and its reset time
	// on every turn but no longer reports a percentage, so an unknown share is
	// the normal case rather than an error — and 0% would be a lie.
	Utilization float64
	Known       bool
	ResetsAt    time.Time // zero when the backend didn't say
	Status      string    // the backend's own word for it: "allowed", …
	// Surpassed is the warning line the window has crossed, 0.75 and up, sent
	// when the CLI thinks it is worth mentioning. It is not the utilization —
	// only a floor under it — but it is the one figure a headless run gets at
	// the point where the answer starts to matter.
	Surpassed float64
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
	// StopReason is the model's own last word on why it stopped writing.
	// end_turn and tool_use are the ordinary ones; max_tokens means the
	// output ran into its ceiling and refusal means a classifier declined,
	// and both of those are a turn that stopped rather than finished.
	StopReason string
	RateLimit  *RateLimit
	Canceled   bool
	Err        string // non-empty ⇒ the turn failed
	// RateLimits is every usage window the turn's last rate_limit_event
	// reported. Newer CLIs put both windows, percentages included, in the
	// event's unifiedWindows; RateLimit above keeps the legacy single window
	// so older saved conversations still decode.
	RateLimits []RateLimit
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
	Effort     string // "" = the CLI's configured default
}

// DefaultEffort means "whatever the CLI is already configured to use".
const DefaultEffort = ""

// EffortChooser is a backend that can be told how hard to think about a turn.
//
// Effort is worth reaching for because it is the only lever on the half of the
// bill that caching cannot touch. Measured over this machine's own calls,
// output is 8.6% of what a turn costs on Opus 5 and 25% on Fable 5.1, where
// thinking is always on and cannot be switched off — and every one of those
// tokens is billed at the highest rate on the price list. Cheaper models are
// the other answer, but a lower effort on a better model is usually the one
// that keeps the quality.
//
// Optional: a backend that has no such setting simply isn't asked.
type EffortChooser interface {
	// Efforts lists the levels this backend accepts, least first.
	// DefaultEffort is always the first entry.
	Efforts() []string
	// DescribeEffort says what a level is for, for the picker.
	DescribeEffort(level string) string
}

// ModelDescriber is a backend that can say what its model aliases resolve to
// and what each is good for. Optional: a backend without notes lists bare
// names, which is what crema did before any of them had anything to say.
type ModelDescriber interface {
	DescribeModel(model string) string
}

// UsageReporter is a backend that can say how much of the subscription's
// allowance is gone. Separate from Agent because it is a property of the
// account rather than of a turn, and not every backend has one.
type UsageReporter interface {
	Usage() []RateLimit
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
	// Commands lists the slash commands and skills installed for dir, so crema
	// can offer them instead of making the user remember what is there.
	Commands(dir string) []Command
	Run(ctx context.Context, opts RunOptions) (<-chan Event, error)
}
