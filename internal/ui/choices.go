package ui

import (
	"regexp"
	"strings"
)

// When the agent finishes a turn by asking something and listing the answers,
// crema turns that list into a picker: arrow to one, press enter, and the
// answer goes back as the next message. That is the shape of Claude Code's own
// AskUserQuestion — except the CLI never offers that tool to a headless
// caller, so the question arrives as ordinary prose and this is what reads it.

// maxChoices caps the picker. A longer list is a menu, not a question, and is
// better read in the conversation than crammed above the input.
const maxChoices = 9

// optionLine matches one row of a list the agent wrote: "1. dark",
// "2) light", "- follow the terminal", "* ask", "a) dark". The bullet is
// spelled as an escape because crema never draws it — this reads what the
// agent wrote, and the font guard only speaks for what crema puts on screen.
var optionLine = regexp.MustCompile(`^\s{0,4}(?:\d{1,2}[.)]|[-*\x{2022}]|\(?[a-zA-Z][.)])\s+(\S.*)$`)

// ParseChoices reads the answers out of a message that ends by asking
// something and listing them. It is deliberately hard to trigger: the list has
// to be the last thing in the message, at least two rows long, and there has
// to be a question mark before it. Any prose in between — a sentence between
// the options, a closing remark after them — means this was a list the agent
// was talking about rather than one it wants an answer to.
func ParseChoices(text string) []string {
	lines := strings.Split(strings.TrimRight(text, " \t\n"), "\n")

	var options []string
	last := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], " \t")
		if line == "" && len(options) == 0 {
			last = i // trailing blank lines are not the end of the list
			continue
		}
		m := optionLine.FindStringSubmatch(line)
		if m == nil {
			break
		}
		options = append([]string{cleanOption(m[1])}, options...)
		last = i
	}
	if len(options) < 2 || len(options) > maxChoices {
		return nil
	}
	if !strings.Contains(strings.Join(lines[:last], "\n"), "?") {
		return nil // a list with no question above it is not a question
	}
	return options
}

// cleanOption strips the markdown an option is usually dressed in, so what
// goes back to the agent reads like something a person typed.
func cleanOption(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(s)
}

// Choices is the picker for those answers. It sits where the completion list
// sits, and behaves the same way: ↑↓ to move, enter to take one, esc to
// dismiss — and typing anything dismisses it too, because a question with
// options is still a question you can answer in your own words.
type Choices struct {
	rows []string
	idx  int
}

func NewChoices(options []string) *Choices {
	if len(options) == 0 {
		return nil
	}
	return &Choices{rows: options}
}

func (c *Choices) Move(delta int) {
	c.idx = min(max(c.idx+delta, 0), len(c.rows)-1)
}

// Selected is the answer enter would send.
func (c *Choices) Selected() string { return c.rows[c.idx] }

// Height is the rows the box occupies: border, the options, the hint line.
func (c *Choices) Height() int { return len(c.rows) + 3 }

// RowAt maps a row inside the box to an option, -1 for the border and hint.
func (c *Choices) RowAt(row int) int {
	if i := row - 1; i >= 0 && i < len(c.rows) {
		return i
	}
	return -1
}

// Click moves onto a clicked option and reports whether it was one.
func (c *Choices) Click(row int) bool {
	if i := c.RowAt(row); i >= 0 {
		c.idx = i
		return true
	}
	return false
}

func (c *Choices) View(w int) string {
	return renderDropUp(c.rows, c.idx, "the agent asked · ↑↓ move · enter answer · esc or type your own", w)
}
