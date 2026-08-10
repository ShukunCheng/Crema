package agent

import (
	"bytes"
	"encoding/json"
)

// ClaudeParser turns `claude --output-format stream-json` lines into Events.
// Unknown top-level types and malformed lines are counted and skipped, never fatal.
type ClaudeParser struct {
	sessionID string
	Skipped   int
}

type claudeLine struct {
	Type         string         `json:"type"`
	Subtype      string         `json:"subtype"`
	SessionID    string         `json:"session_id"`
	Message      *claudeMessage `json:"message"`
	IsError      bool           `json:"is_error"`
	DurationMS   int64          `json:"duration_ms"`
	TotalCostUSD float64        `json:"total_cost_usd"`
	Result       string         `json:"result"`
	Usage        *claudeUsage   `json:"usage"`
}

type claudeMessage struct {
	Content []claudeBlock `json:"content"`
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
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
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
		return nil // init records the session id above; other subtypes are tolerated
	case "assistant", "user":
		if l.Message == nil {
			return nil
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
		return evs
	case "result":
		res := TurnResult{SessionID: p.sessionID, DurationMS: l.DurationMS, CostUSD: l.TotalCostUSD}
		if l.Usage != nil {
			res.InputTokens, res.OutputTokens = l.Usage.InputTokens, l.Usage.OutputTokens
		}
		if l.IsError {
			res.Err = l.Result
			if res.Err == "" {
				res.Err = "claude reported an error result"
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
