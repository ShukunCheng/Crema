package agent

import (
	"bytes"
	"encoding/json"
	"strings"
)

// CodexParser understands both the current `{"type":"item.*"}` schema and the
// legacy `{"id":…,"msg":{…}}` one. Unknown types are counted and skipped.
type CodexParser struct {
	threadID string
	started  map[string]bool // item ids whose ToolCall we already emitted
	inTok    int64           // legacy token_count carry-over
	outTok   int64
	Skipped  int
}

type codexLine struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Message  string          `json:"message"` // top-level error
	Item     *codexItem      `json:"item"`
	Usage    *codexUsage     `json:"usage"`
	Error    *codexErr       `json:"error"`
	Msg      json.RawMessage `json:"msg"` // legacy schema
}

type codexItem struct {
	ID       string `json:"id"`
	ItemType string `json:"item_type"`
	Text     string `json:"text"`
	// Command is raw because codex has shipped it both as a string and as an
	// argv array; a typed field would make the whole event unparseable.
	Command          json.RawMessage `json:"command"`
	AggregatedOutput string          `json:"aggregated_output"`
	ExitCode         *int            `json:"exit_code"`
	Status           string          `json:"status"`
	Changes          json.RawMessage `json:"changes"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Query            string          `json:"query"`
}

type codexUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
}

type codexErr struct {
	Message string `json:"message"`
}

type codexLegacyMsg struct {
	Type             string   `json:"type"`
	Message          string   `json:"message"`
	Text             string   `json:"text"`
	CallID           string   `json:"call_id"`
	Command          []string `json:"command"`
	Stdout           string   `json:"stdout"`
	Stderr           string   `json:"stderr"`
	ExitCode         int      `json:"exit_code"`
	InputTokens      int64    `json:"input_tokens"`
	OutputTokens     int64    `json:"output_tokens"`
	LastAgentMessage string   `json:"last_agent_message"`
}

func (p *CodexParser) SessionID() string { return p.threadID }

func (p *CodexParser) ParseLine(line []byte) []Event {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var l codexLine
	if err := json.Unmarshal(line, &l); err != nil {
		p.Skipped++
		return nil
	}
	if len(l.Msg) > 0 {
		return p.parseLegacy(l.Msg)
	}
	switch l.Type {
	case "thread.started":
		p.threadID = l.ThreadID
		return nil
	case "turn.started", "item.updated":
		return nil
	case "item.started":
		return p.itemStarted(l.Item)
	case "item.completed":
		return p.itemCompleted(l.Item)
	case "error":
		return []Event{{Kind: KindError, Text: l.Message}}
	case "turn.completed":
		res := TurnResult{SessionID: p.threadID}
		if l.Usage != nil {
			res.InputTokens, res.OutputTokens = l.Usage.InputTokens, l.Usage.OutputTokens
		}
		return []Event{{Kind: KindTurnEnd, Result: &res}}
	case "turn.failed":
		res := TurnResult{SessionID: p.threadID, Err: "codex turn failed"}
		if l.Error != nil && l.Error.Message != "" {
			res.Err = l.Error.Message
		}
		return []Event{{Kind: KindTurnEnd, Result: &res}}
	default:
		p.Skipped++
		return nil
	}
}

func (p *CodexParser) itemStarted(it *codexItem) []Event {
	if it == nil {
		return nil
	}
	if it.ItemType == "command_execution" {
		if p.started == nil {
			p.started = map[string]bool{}
		}
		p.started[it.ID] = true
		return []Event{{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: "shell", Input: commandString(it.Command)}}}
	}
	return nil // reasoning/agent_message render once, on completion
}

// commandString accepts either "ls -la" or ["bash","-lc","ls -la"].
func commandString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var argv []string
	if json.Unmarshal(raw, &argv) == nil {
		return strings.Join(argv, " ")
	}
	return string(raw)
}

func (p *CodexParser) itemCompleted(it *codexItem) []Event {
	if it == nil {
		return nil
	}
	switch it.ItemType {
	case "agent_message":
		return []Event{{Kind: KindText, Text: it.Text}}
	case "reasoning":
		return []Event{{Kind: KindText, Thinking: true, Text: it.Text}}
	case "command_execution":
		var evs []Event
		if !p.started[it.ID] {
			evs = append(evs, Event{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: "shell", Input: commandString(it.Command)}})
		}
		isErr := it.ExitCode != nil && *it.ExitCode != 0
		evs = append(evs, Event{Kind: KindToolOutput, Output: &ToolOutput{ToolID: it.ID, Content: it.AggregatedOutput, IsError: isErr}})
		return evs
	case "file_change":
		return []Event{
			{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: "apply_patch", Input: prettyJSON(it.Changes)}},
			{Kind: KindToolOutput, Output: &ToolOutput{ToolID: it.ID, Content: it.Status}},
		}
	case "mcp_tool_call":
		name := strings.TrimPrefix(it.Server+"."+it.Tool, ".")
		return []Event{{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: name, Input: it.Text}}}
	case "web_search":
		return []Event{{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: "web_search", Input: it.Query}}}
	case "error":
		return []Event{{Kind: KindError, Text: it.Text}}
	default: // todo_list and future item types
		return nil
	}
}

func (p *CodexParser) parseLegacy(raw json.RawMessage) []Event {
	var m codexLegacyMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		p.Skipped++
		return nil
	}
	switch m.Type {
	case "agent_message":
		return []Event{{Kind: KindText, Text: m.Message}}
	case "agent_reasoning":
		return []Event{{Kind: KindText, Thinking: true, Text: m.Text}}
	case "exec_command_begin":
		return []Event{{Kind: KindToolCall, Tool: &ToolCall{ID: m.CallID, Name: "shell", Input: strings.Join(m.Command, " ")}}}
	case "exec_command_end":
		out := m.Stdout
		if m.Stderr != "" {
			out += m.Stderr
		}
		return []Event{{Kind: KindToolOutput, Output: &ToolOutput{ToolID: m.CallID, Content: out, IsError: m.ExitCode != 0}}}
	case "token_count":
		p.inTok, p.outTok = m.InputTokens, m.OutputTokens
		return nil
	case "task_complete":
		return []Event{{Kind: KindTurnEnd, Result: &TurnResult{
			SessionID: p.threadID, InputTokens: p.inTok, OutputTokens: p.outTok}}}
	case "error":
		return []Event{{Kind: KindError, Text: m.Message}}
	case "task_started":
		return nil
	default:
		p.Skipped++
		return nil
	}
}
