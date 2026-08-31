package agent

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// ClaudeParser turns `claude --output-format stream-json` lines into Events.
// Unknown top-level types and malformed lines are counted and skipped, never fatal.
type ClaudeParser struct {
	sessionID string
	rate      *RateLimit  // the legacy single window of the latest rate_limit_event
	rates     []RateLimit // its unifiedWindows: every window, percentages included
	costTotal float64     // the run's cumulative bill so far; results report growth
	// context is what the last API call of the turn had to read. Every
	// assistant message carries the usage of the call that produced it, and
	// the last one is the conversation's size now.
	context int64
	// outTokens is how much the model has written this turn so far, counted
	// once per API call; lastMsg is how a repeat is spotted.
	outTokens int64
	lastMsg   string
	Skipped   int
}

type claudeLine struct {
	Type         string         `json:"type"`
	Subtype      string         `json:"subtype"`
	ParentTool   string         `json:"parent_tool_use_id"`
	TaskID       string         `json:"task_id"`
	ToolUseID    string         `json:"tool_use_id"`
	Description  string         `json:"description"`
	SubagentType string         `json:"subagent_type"`
	TaskType     string         `json:"task_type"`
	LastToolName string         `json:"last_tool_name"`
	Status       string         `json:"status"`
	OutputFile   string         `json:"output_file"`
	Summary      string         `json:"summary"`
	Patch        *statusPatch   `json:"patch"`
	SessionID    string         `json:"session_id"`
	Message      *claudeMessage `json:"message"`
	IsError      bool           `json:"is_error"`
	DurationMS   int64          `json:"duration_ms"`
	TotalCostUSD float64        `json:"total_cost_usd"`
	Result       string         `json:"result"`
	Usage        *claudeUsage   `json:"usage"`
	// modelUsage is keyed by model id; we only need the context window, which
	// is the same for every entry of a single turn.
	ModelUsage    map[string]claudeModelUsage `json:"modelUsage"`
	RateLimitInfo *claudeRateLimit            `json:"rate_limit_info"`
	// SlashCommands is the init message's own list of what the CLI can be
	// asked for — built-ins, plugin commands and skills alike. It is the only
	// authoritative source: crema would otherwise be guessing from the
	// filesystem and from memory.
	SlashCommands []string `json:"slash_commands"`
}

type claudeModelUsage struct {
	ContextWindow int64 `json:"contextWindow"`
}

type claudeRateLimit struct {
	Status        string   `json:"status"`
	RateLimitType string   `json:"rateLimitType"`
	Utilization   *float64 `json:"utilization"` // absent while usage is ordinary
	ResetsAt      int64    `json:"resetsAt"`    // unix seconds
	// SurpassedThreshold is the CLI's own warning line — 0.75 and up — sent
	// once the window is far enough along to say so.
	SurpassedThreshold float64 `json:"surpassedThreshold"`
	// UnifiedWindows arrived after the top-level utilization went away: every
	// window at once, percentage and reset included, on each event. Measured
	// live — {"five_hour":{"utilization":0.53,"resetsAt":...},"seven_day":…}.
	UnifiedWindows map[string]struct {
		Utilization float64 `json:"utilization"` // 0..1
		ResetsAt    int64   `json:"resetsAt"`    // unix seconds
	} `json:"unifiedWindows"`
}

type claudeMessage struct {
	ID      string        `json:"id"`
	Content []claudeBlock `json:"content"`
	// Usage is per API call, not per turn: this is what the model read to
	// produce this one message.
	Usage *claudeUsage `json:"usage"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

type claudeUsage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheReadTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
	// TotalTokens and ToolUses only appear on task_progress lines, whose
	// usage is a subagent's running bill rather than an API call's.
	TotalTokens int64 `json:"total_tokens"`
	ToolUses    int   `json:"tool_uses"`
}

// statusPatch is the task_updated line's delta.
type statusPatch struct {
	Status string `json:"status"`
}

// context is everything the model had to read this turn — fresh input plus
// both halves of the cache. That sum is what fills the context window.
func (u claudeUsage) context() int64 {
	return u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens
}

func (p *ClaudeParser) SessionID() string { return p.sessionID }

func (p *ClaudeParser) ParseLine(line []byte) []Event {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var l claudeLine
	if err := json.Unmarshal(line, &l); err != nil {
		p.Skipped++
		return nil
	}
	if l.SessionID != "" {
		p.sessionID = l.SessionID
	}
	switch l.Type {
	case "system":
		if l.Subtype == "init" && len(l.SlashCommands) > 0 {
			return []Event{{Kind: KindReady, Commands: l.SlashCommands}}
		}
		switch l.Subtype {
		case "task_started", "task_progress", "task_updated", "task_notification":
			return []Event{{Kind: KindTask, Task: taskUpdate(&l)}}
		}
		return nil // init records the session id above; other subtypes are tolerated
	case "rate_limit_event":
		if info := l.RateLimitInfo; info != nil {
			legacy := RateLimit{
				Type:      info.RateLimitType,
				Status:    info.Status,
				Surpassed: info.SurpassedThreshold,
			}
			if u := info.Utilization; u != nil {
				legacy.Utilization, legacy.Known = *u, true
			}
			if info.ResetsAt > 0 {
				legacy.ResetsAt = time.Unix(info.ResetsAt, 0)
			}
			p.rate = &legacy // surfaced on the next TurnEnd
			p.rates = nil
			for name, w := range info.UnifiedWindows {
				r := RateLimit{Type: name, Utilization: w.Utilization, Known: true}
				if w.ResetsAt > 0 {
					r.ResetsAt = time.Unix(w.ResetsAt, 0)
				}
				if name == info.RateLimitType {
					r.Status, r.Surpassed = info.Status, info.SurpassedThreshold
				}
				p.rates = append(p.rates, r)
			}
			sort.Slice(p.rates, func(i, j int) bool { return p.rates[i].ResetsAt.Before(p.rates[j].ResetsAt) })
		}
		return nil
	case "assistant", "user":
		if l.Message == nil {
			return nil
		}
		if l.Type == "assistant" && l.Message.Usage != nil && l.ParentTool == "" {
			// Subagent messages carry the subagent's own usage; folding it in
			// here would overwrite the main conversation's context with a
			// side conversation's.
			// Each call re-reads the whole conversation, so the newest of
			// these is how big it has become. Summing them, which is what the
			// result line's own usage does, counts it once per call.
			p.context = l.Message.Usage.context()
			// Output, on the other hand, does add up — but one API call
			// arrives as several lines, one per content block, each repeating
			// that call's usage. The message id is what tells them apart.
			if l.Message.ID != p.lastMsg {
				p.lastMsg = l.Message.ID
				p.outTokens += l.Message.Usage.OutputTokens
			}
		}
		var evs []Event
		for _, b := range l.Message.Content {
			switch b.Type {
			case "thinking":
				if b.Thinking != "" {
					evs = append(evs, Event{Kind: KindText, Thinking: true, Text: b.Thinking})
				}
			case "text":
				if b.Text != "" {
					evs = append(evs, Event{Kind: KindText, Text: b.Text})
				}
			case "tool_use":
				evs = append(evs, Event{Kind: KindToolCall, Tool: &ToolCall{
					ID: b.ID, Name: b.Name, Input: prettyJSON(b.Input),
				}})
			case "tool_result":
				evs = append(evs, Event{Kind: KindToolOutput, Output: &ToolOutput{
					ToolID: b.ToolUseID, Content: flattenContent(b.Content), IsError: b.IsError,
				}})
			}
		}
		for i := range evs {
			evs[i].SubID = l.ParentTool
			if l.Type == "assistant" && l.ParentTool == "" {
				evs[i].OutTokens = p.outTokens
			}
		}
		return evs
	case "result":
		defer func() { p.outTokens, p.lastMsg = 0, "" }() // the next turn counts afresh
		res := TurnResult{
			SessionID: p.sessionID, DurationMS: l.DurationMS,
			CostUSD: l.TotalCostUSD - p.costTotal, RateLimit: p.rate,
			RateLimits: p.rates,
		}
		// One run can end more than once: a turn that launched an async task
		// gets continued when the task finishes, and each leg closes with its
		// own result line — whose total_cost_usd is cumulative for the run.
		// Only the growth is new money.
		if res.CostUSD < 0 {
			res.CostUSD = l.TotalCostUSD
		}
		p.costTotal = l.TotalCostUSD
		if l.Usage != nil {
			res.InputTokens, res.OutputTokens = l.Usage.InputTokens, l.Usage.OutputTokens
		}
		// The result line's usage is the turn's whole bill, added up across
		// every API call it took. What fills the context window is one call's
		// worth, so the last message's is the number to keep.
		res.ContextTokens = p.context
		for _, mu := range l.ModelUsage { // same window for every entry of one turn
			if mu.ContextWindow > 0 {
				res.ContextWindow = mu.ContextWindow
				break
			}
		}
		// is_error alone is not the whole truth: a resume that goes nowhere
		// has been seen ending with an error subtype, is_error false, and an
		// empty result — a clean-looking line for a turn that did nothing.
		if l.IsError || (l.Subtype != "" && l.Subtype != "success") {
			res.Err = l.Result
			if res.Err == "" {
				res.Err = "claude ended the turn with " + strings.ReplaceAll(l.Subtype, "_", " ")
				if l.Subtype == "" {
					res.Err = "claude reported an error result"
				}
			}
		}
		return []Event{{Kind: KindTurnEnd, Result: &res}}
	default:
		p.Skipped++
		return nil
	}
}

// prettyJSON renders tool input for humans: 2-space indent, raw string on failure.
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

// flattenContent handles tool_result.content being a string, a block array, or absent.
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var buf bytes.Buffer
		for i, b := range blocks {
			if i > 0 {
				buf.WriteByte('\n')
			}
			buf.WriteString(b.Text)
		}
		return buf.String()
	}
	return string(raw)
}

// taskUpdate reads one task lifecycle line into the shared shape. The four
// subtypes carry different subsets; whatever a line has is passed along.
func taskUpdate(l *claudeLine) *TaskUpdate {
	u := &TaskUpdate{
		ID: l.TaskID, ToolUseID: l.ToolUseID, Desc: l.Description,
		Type: l.SubagentType, LastTool: l.LastToolName,
		OutputFile: l.OutputFile, Summary: l.Summary, Status: l.Status,
	}
	if u.Type == "" {
		u.Type = l.TaskType
	}
	switch l.Subtype {
	case "task_started", "task_progress":
		u.Status = "running"
	}
	if u.Status == "" && l.Patch != nil {
		u.Status = l.Patch.Status
	}
	if l.Subtype == "task_progress" && l.Usage != nil {
		u.Tokens, u.ToolUses = l.Usage.TotalTokens, l.Usage.ToolUses
	}
	return u
}
