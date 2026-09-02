package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// A conversation that grows without limit is the last expensive thing about
// driving the CLI headlessly, and it is expensive on every single turn:
// measured across this machine's agents, the median conversation was 714k
// tokens by the time a turn started, and the largest 988k of a 1M window.
// Every turn re-reads all of it, and every process that has to be opened cold
// — a restart, a mode change, a night's gap — rewrites all of it.
//
// The CLI's own interactive sessions compact themselves before that happens.
// A headless run never does; nobody is there to be asked. So crema does it,
// at the same point and by the same means as /compact: summarise what has
// happened, start the conversation again with that summary in front of the
// next message. Announced in the transcript, and switchable per agent.

// compactAt is how full the window has to be. Claude Code's own autocompact
// fires around here; below it the saving does not pay for the summarising
// turn, and above it a long turn risks meeting the hard limit mid-answer.
const compactAt = 0.8

// autoCompactDue reports whether this agent has grown enough to be worth
// folding up. Only ever checked when the agent is idle: compacting drops the
// conversation, which is not a thing to do underneath a running turn.
func (s *Session) autoCompactDue() bool {
	if !s.AutoCompact || s.busy || s.compacting {
		return false
	}
	if s.tl.Len() <= 1 {
		return false // nothing to summarise
	}
	return contextFrac(s.ctxTokens, s.ctxWindow) >= compactAt
}

// maybeAutoCompact folds the conversation up when it has grown too big to
// keep paying for. It returns the command that does the summarising, or nil
// when there is nothing to do.
func (a *App) maybeAutoCompact(s *Session) tea.Cmd {
	if !s.autoCompactDue() {
		return nil
	}
	pct := int(contextFrac(s.ctxTokens, s.ctxWindow)*100 + 0.5)
	s.tl.Append(Block{Kind: BlockSystem, Text: fmt.Sprintf(
		"the conversation is %d%% of the window (%d tokens), which every turn "+
			"pays to re-read — compacting it now, the way an interactive session "+
			"would. /autocompact off stops this.", pct, s.ctxTokens)})
	s.compacting = true
	return tea.Batch(s.startTurn("/compact", compactPrompt), a.sp.Tick)
}

// runAutoCompact is the /autocompact command: crema's own, since the CLI's
// belongs to an interface a headless run does not have.
func runAutoCompact(a *App, s *Session, arg string) tea.Cmd {
	switch arg {
	case "on":
		s.AutoCompact = true
	case "off":
		s.AutoCompact = false
	case "":
		// Report, and say what it would do next.
		state := "off"
		if s.AutoCompact {
			state = "on"
		}
		line := "autocompact is " + state
		if f := contextFrac(s.ctxTokens, s.ctxWindow); f >= 0 {
			line += fmt.Sprintf(" · the conversation is %d%% of the window, folding up at %d%%",
				int(f*100+0.5), int(compactAt*100))
		}
		s.tl.Append(Block{Kind: BlockSystem, Text: line})
		return nil
	default:
		a.note = "/autocompact on, or /autocompact off"
		return nil
	}
	a.persist()
	if s.AutoCompact {
		a.note = "autocompact on — this agent folds itself up at " +
			fmt.Sprintf("%d%% of the window", int(compactAt*100))
		return a.maybeAutoCompact(s)
	}
	a.note = "autocompact off — this conversation grows until you /compact it"
	return nil
}
