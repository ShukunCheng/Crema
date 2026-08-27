package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A headless run continues a conversation with --resume <id>, and the CLI
// keeps every conversation it has ever had in ~/.claude/projects, one
// directory per working directory. Interactively /resume is the CLI's own
// picker over that directory; crema's /resume reads the same files, so an
// agent can be pointed at any conversation this project has had — the CLI's
// or crema's own.

// SessionInfo is one saved conversation, as much of it as a list needs.
type SessionInfo struct {
	ID      string
	When    time.Time // when the conversation last moved
	Preview string    // the first thing the user said in it
}

// SessionLister is the optional backend interface behind /resume: a backend
// that keeps its conversations somewhere crema can read lists them here,
// newest first.
type SessionLister interface {
	Sessions(dir string) []SessionInfo
}

// projectSlug is how the CLI names a project's directory under
// ~/.claude/projects: every byte that is not a letter or digit becomes a
// dash. Measured, not guessed — D:\Crema is D--Crema, and a temp path
// with backslashes, colons and dots came out all dashes.
func projectSlug(dir string) string {
	var b strings.Builder
	for _, r := range dir {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Sessions lists the CLI's saved conversations for dir, newest first.
func (c *Claude) Sessions(dir string) []SessionInfo {
	home, err := homeDir()
	if err != nil {
		return nil
	}
	pattern := filepath.Join(home, ".claude", "projects", projectSlug(dir), "*.jsonl")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	var out []SessionInfo
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		out = append(out, SessionInfo{
			ID:      strings.TrimSuffix(filepath.Base(p), ".jsonl"),
			When:    fi.ModTime(),
			Preview: sessionPreview(p),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.After(out[j].When) })
	return out
}

// sessionPreview is the first thing the user said in a transcript — the one
// line that tells conversations apart in a list. Only the head of the file is
// read: the opening message is at the start, and some transcripts run to
// megabytes.
func sessionPreview(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for n := 0; sc.Scan() && n < 200; n++ {
		var l struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message *struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Type != "user" || l.IsMeta || l.Message == nil {
			continue
		}
		// The content is a plain string for a typed message, and an array of
		// blocks for tool results — which are not what anyone means by "what
		// was this conversation about".
		var text string
		if json.Unmarshal(l.Message.Content, &text) == nil {
			if t := firstLine(text); t != "" {
				return t
			}
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
