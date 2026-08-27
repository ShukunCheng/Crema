package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The enter key is the most overloaded key crema has: the same keystroke is a
// send, a newline continuing a shift or ctrl chord, or one character of a
// paste the Windows console is replaying — which arrives with the modifier
// thrown away and nothing marking it pasted. Every enter goes through one
// judgment, and /keytest holds that judgment still so it can be looked at:
// each scenario is a set of facts put to the real function, not a story about
// it.

// enterFacts is everything crema knows about one press of enter at the moment
// it has to decide.
type enterFacts struct {
	paste   bool // the terminal said outright this is pasted text
	ctrl    bool // a control key was down
	shift   bool // a shift key was down
	pressed bool // the OS saw the key itself go down — a replay never presses it
	burst   int  // keys before this one that arrived faster than typing
	gapMS   int  // milliseconds since the previous key; -1 when it is the first
}

// judgeEnter is that judgment: newline, or send. The reason is in words
// because /keytest prints it; the caller acting on the verdict ignores it.
func judgeEnter(f enterFacts) (newline bool, why string) {
	switch {
	case f.ctrl:
		return true, "ctrl was down"
	case f.shift:
		return true, "shift was down"
	case f.paste:
		return true, "the terminal marked it pasted"
	case f.burst >= 2:
		return true, "it arrived mid-burst, faster than anyone types"
	case f.gapMS >= 0 && time.Duration(f.gapMS)*time.Millisecond < pasteBurst && !f.pressed:
		return true, "hot on the last key's heels, and the key was never pressed"
	}
	return false, "nothing marked it a paste and no modifier was down"
}

// keyScenarios are the situations worth putting to judgeEnter, each an answer
// to "why didn't that send?". The table is also the unit test: a build whose
// judgment disagrees with a wants column fails in CI, so what /keytest shows
// the user is never out of date.
var keyScenarios = []struct {
	name string
	told string // what happened at the keyboard, in words
	f    enterFacts
	want bool // newline?
}{
	{"plain", "a pause, then enter, seen going down",
		enterFacts{pressed: true, gapMS: 400}, false},
	{"lag", "typed fast and released before crema looked — the OS still remembers the press",
		enterFacts{pressed: true, gapMS: 90}, false},
	{"replay", "mid-paste: keys milliseconds apart, enter never pressed",
		enterFacts{burst: 5, gapMS: 8}, true},
	{"trail", "a paste's own trailing newline: close behind the rest, never pressed",
		enterFacts{gapMS: 90}, true},
	{"bracket", "a terminal that brackets pastes said so",
		enterFacts{paste: true, pressed: true, gapMS: 400}, true},
	{"shift", "shift held: a newline was asked for",
		enterFacts{shift: true, pressed: true, gapMS: 400}, true},
	{"ctrl", "ctrl held: a newline was asked for",
		enterFacts{ctrl: true, pressed: true, gapMS: 400}, true},
}

// runKeytest is the /keytest builtin. Bare, it lists the scenarios and puts
// them in the option picker — the one the agent's own questions use — so a
// scenario is an arrow and an enter away. Named, it runs one; /keytest all
// runs the table.
func runKeytest(a *App, s *Session, arg string) tea.Cmd {
	if arg == "" {
		var b strings.Builder
		b.WriteString("key test — every enter goes through one judgment; put a scenario to it:\n")
		for _, sc := range keyScenarios {
			fmt.Fprintf(&b, "  %-8s %s\n", sc.name, sc.told)
		}
		b.WriteString("  all      the whole table")
		s.tl.Append(Block{Kind: BlockSystem, Text: b.String()})
		opts := make([]string, 0, len(keyScenarios)+1)
		for _, sc := range keyScenarios {
			opts = append(opts, "/keytest "+sc.name)
		}
		a.choices = NewChoices(append(opts, "/keytest all"))
		return nil
	}
	var lines []string
	for _, sc := range keyScenarios {
		if strings.EqualFold(arg, "all") || strings.EqualFold(arg, sc.name) {
			newline, why := judgeEnter(sc.f)
			verdict, wanted := verdictWord(newline), verdictWord(sc.want)
			line := fmt.Sprintf("%-8s %s\n         verdict: %s — %s", sc.name, sc.told, verdict, why)
			if newline != sc.want {
				line += fmt.Sprintf("\n         WRONG — this build expected %s; please report it", wanted)
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		a.note = arg + " is not a scenario — /keytest lists them"
		return nil
	}
	s.tl.Append(Block{Kind: BlockSystem, Text: strings.Join(lines, "\n")})
	return nil
}

func verdictWord(newline bool) string {
	if newline {
		return "newline"
	}
	return "send"
}
