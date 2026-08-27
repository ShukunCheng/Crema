package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// /resume points the agent at any conversation this project has had — the
// CLI's own, or one crema started. Interactively that is the CLI's session
// picker; headless the CLI does not offer it, so crema reads the same files
// the CLI's picker does and offers the same choice.

// resumeListed caps the list at what the option picker can number.
const resumeListed = 9

func runResume(a *App, s *Session, arg string) tea.Cmd {
	lister, ok := s.Backend.(agent.SessionLister)
	if !ok {
		a.note = s.Backend.Label() + " does not keep conversations where crema can list them"
		return nil
	}
	infos := lister.Sessions(s.Dir)
	if len(infos) == 0 {
		a.note = "no saved conversations for " + s.Dir
		return nil
	}
	if arg != "" {
		for _, inf := range infos {
			if !strings.HasPrefix(inf.ID, arg) {
				continue
			}
			if inf.ID == s.agentSID {
				a.note = "this agent is already on that conversation"
				return nil
			}
			s.agentSID = inf.ID
			// Size and spend belong to the conversation left behind; the
			// next turn reports this one's.
			s.ctxTokens, s.ctxWindow = 0, 0
			line := "the next message continues conversation " + inf.ID
			if inf.Preview != "" {
				line += ", which began: " + inf.Preview
			}
			s.tl.Append(Block{Kind: BlockSystem, Text: line})
			a.persist()
			return nil
		}
		a.note = arg + " matches no saved conversation — /resume lists them"
		return nil
	}

	var b strings.Builder
	b.WriteString("this project's saved conversations, newest first:\n")
	shown := infos
	if len(shown) > resumeListed {
		shown = shown[:resumeListed]
	}
	opts := make([]string, 0, len(shown))
	for i, inf := range shown {
		mark := ""
		if inf.ID == s.agentSID {
			mark = " — the one this agent is on"
		}
		fmt.Fprintf(&b, "  %d. %s · %s · %s%s\n", i+1, inf.ID[:8], ago(time.Since(inf.When)), clip(inf.Preview, 48), mark)
		opts = append(opts, "/resume "+inf.ID[:8])
	}
	if n := len(infos) - len(shown); n > 0 {
		fmt.Fprintf(&b, "  … and %d older\n", n)
	}
	b.WriteString("pick one, or /resume <id> — /clear leaves a conversation without picking another")
	s.tl.Append(Block{Kind: BlockSystem, Text: b.String()})
	a.choices = NewChoices(opts)
	return nil
}

// ago says how long past a moment is, in the one unit a list wants.
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
