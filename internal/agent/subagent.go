package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// A subagent keeps a transcript of its own, and the CLI writes it somewhere
// crema can read:
//
//	~/.claude/projects/<project>/<session id>/subagents/agent-<task id>.jsonl
//
// beside an agent-<task id>.meta.json saying what kind of agent it is and
// what it was asked for. Both halves of that path are things crema already
// holds: the session id and the task id arrive together on the task_started
// line, and the project directory is the slug of the working directory.
// Measured against 417 of these on this machine, and against crema's own
// captures — the task_id in claude_subagent.jsonl is exactly the filename of
// the transcript the run left behind.
//
// This matters because of what the alternative was. crema used to find out
// what a subagent was doing by asking the main agent to relay it, which costs
// a whole turn of the most expensive conversation on the machine and depends
// on the model choosing to answer. The file costs nothing, it is live while
// the subagent is running, and it is the same transcript the CLI's own
// subagent panel is showing.

// SubagentMeta is the sidecar: what the subagent is and what it was handed.
type SubagentMeta struct {
	Type       string `json:"agentType"`
	Desc       string `json:"description"`
	ToolUseID  string `json:"toolUseId"`
	SpawnDepth int    `json:"spawnDepth"`
}

// SubagentLog tails one subagent's transcript. It remembers how far it has
// read, so the second call returns only what has been written since the
// first — which is what makes polling it cheap enough to do while the
// subagent runs.
type SubagentLog struct {
	Path string
	Meta SubagentMeta

	// off is how far into the file has been read, always landing on a
	// newline. A line the writer has not finished is simply not counted, and
	// the next read starts again at the beginning of it.
	off int64
}

// ErrNoSubagentLog says the transcript is not there. A subagent that has only
// just started has not been given a file yet, so this is an ordinary
// answer and not a failure — try again on the next tick.
var ErrNoSubagentLog = errors.New("no transcript for this subagent yet")

// SubagentPath is where the CLI keeps one subagent's transcript.
func SubagentPath(dir, sessionID, taskID string) (string, error) {
	if sessionID == "" || taskID == "" {
		return "", ErrNoSubagentLog
	}
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".claude", "projects")
	name := "agent-" + taskID + ".jsonl"
	p := filepath.Join(root, projectSlug(dir), sessionID, "subagents", name)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	// The slug is measured rather than documented, and a session can be
	// resumed from a directory that is not the one it started in. The session
	// id is unique on its own, so look for it under any project before
	// giving up.
	hits, _ := filepath.Glob(filepath.Join(root, "*", sessionID, "subagents", name))
	if len(hits) > 0 {
		return hits[0], nil
	}
	return "", ErrNoSubagentLog
}

// OpenSubagentLog finds a subagent's transcript and reads its sidecar. It
// reads none of the transcript itself: Read does that, and does it again.
func OpenSubagentLog(dir, sessionID, taskID string) (*SubagentLog, error) {
	p, err := SubagentPath(dir, sessionID, taskID)
	if err != nil {
		return nil, err
	}
	l := &SubagentLog{Path: p}
	if b, err := os.ReadFile(strings.TrimSuffix(p, ".jsonl") + ".meta.json"); err == nil {
		_ = json.Unmarshal(b, &l.Meta) // the sidecar is a nicety, not a requirement
	}
	return l, nil
}

// subLine is one line of a subagent transcript. It is the CLI's on-disk
// format rather than its stream-json output, and the difference that matters
// is content: an assistant message carries an array of blocks, a user message
// carries either an array of tool results or a bare string.
type subLine struct {
	Type    string `json:"type"`
	Message *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Usage   *claudeUsage    `json:"usage"`
	} `json:"message"`
}

// Read returns everything written since the last call. Growing files are the
// normal case; a file that has shrunk has been replaced, so it is read from
// the top again rather than from an offset that now means something else.
func (l *SubagentLog) Read() ([]Event, error) {
	f, err := os.Open(l.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() < l.off {
		l.off = 0
	}
	if fi.Size() == l.off {
		return nil, nil
	}
	if _, err := f.Seek(l.off, io.SeekStart); err != nil {
		return nil, err
	}
	var evs []Event
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			// A whole line: count it as read, and take what it says.
			l.off += int64(len(line))
			evs = append(evs, subEvents(line)...)
		}
		// A line with no newline on the end is one the writer is still in the
		// middle of. It goes uncounted, so the next read starts again at its
		// first byte and gets it whole.
		if err != nil {
			break
		}
	}
	return evs, nil
}

// subEvents turns one transcript line into what the timeline draws. Lines it
// does not know — the attachment records the CLI writes between messages, the
// system notes — are silently nothing, the same way the stream parser treats
// a type it has not met.
func subEvents(line []byte) []Event {
	line = trimSpaceBytes(line)
	if len(line) == 0 {
		return nil
	}
	var l subLine
	if err := json.Unmarshal(line, &l); err != nil || l.Message == nil {
		return nil
	}
	switch l.Type {
	case "user":
		// Either the prompt the subagent was given — a bare string, and the
		// first thing in every one of these files — or the results of the
		// tools it just ran.
		var text string
		if json.Unmarshal(l.Message.Content, &text) == nil {
			if strings.TrimSpace(text) == "" {
				return nil
			}
			return []Event{{Kind: KindUser, Text: text}}
		}
	case "assistant":
	default:
		return nil
	}
	var blocks []claudeBlock
	if err := json.Unmarshal(l.Message.Content, &blocks); err != nil {
		return nil
	}
	var evs []Event
	for _, b := range blocks {
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
}

func trimSpaceBytes(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t') {
		b = b[1:]
	}
	return b
}
