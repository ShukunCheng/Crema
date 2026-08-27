package ui

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The two things you change mid-conversation — which model answers and what it
// is allowed to do — used to open a full-screen panel. Changing a setting is
// not a place you go; it is a button you press. So they are buttons, on a row
// above the input, and each opens a list where the completions appear.
type controlKind int

const (
	controlModel controlKind = iota
	controlPermission
)

// controlOption is one value a chip can take.
type controlOption struct {
	kind        controlKind
	label, desc string
	perm        agent.PermissionMode
	model       string
	on          bool
}

// controlChip is one button: what it sets, what it is set to, and what else it
// could be set to.
type controlChip struct {
	kind    controlKind
	label   string
	value   string
	options []controlOption
	// col and width are where the chip was drawn, so a click can find it.
	col, width int
}

// Controls is the button row above the input, and the list one of its buttons
// has open.
type Controls struct {
	chips []controlChip
	idx   int  // the highlighted button
	open  bool // its list is showing
	opt   int  // the highlighted row of that list
}

// NewControls builds the row for one agent, offering only what its backend can
// actually honour.
func NewControls(s *Session) *Controls {
	c := &Controls{}

	model := controlChip{kind: controlModel, label: "model", value: modelLabel(s.Model)}
	describer, _ := s.Backend.(agent.ModelDescriber)
	for _, m := range s.Backend.Models() {
		label, desc := modelLabel(m), ""
		if describer != nil {
			desc = describer.DescribeModel(m)
		}
		if m == agent.DefaultModel && desc == "" {
			desc = "whatever the CLI is configured to use"
		}
		model.options = append(model.options, controlOption{
			kind: controlModel, label: label, desc: desc, model: m, on: m == s.Model,
		})
	}

	perm := controlChip{kind: controlPermission, label: "permissions", value: s.Permission.Label()}
	for _, m := range s.Backend.Modes() {
		perm.options = append(perm.options, controlOption{
			kind: controlPermission, label: m.Label(), desc: m.Describe(),
			perm: m, on: m == s.Permission,
		})
	}

	c.chips = []controlChip{model, perm}
	c.layout()
	return c
}

// layout works out where each button sits. Computed here rather than while
// drawing, so a click can be placed before the first frame — and recomputed
// whenever a value changes, since the text is what sets the width.
func (c *Controls) layout() {
	col := 1
	for i := range c.chips {
		c.chips[i].col = col
		c.chips[i].width = lipgloss.Width(c.chipText(i))
		col += c.chips[i].width + 1
	}
}

func (c *Controls) chipText(i int) string {
	return " " + c.chips[i].label + "  " + c.chips[i].value + " "
}

func modelLabel(m string) string {
	if m == agent.DefaultModel {
		return "default"
	}
	return m
}

// Open reports whether a list is showing, which is what decides who owns the
// arrow keys.
func (c *Controls) Open() bool { return c.open }

// Update handles a key. chosen is a value to apply; closed means the row is
// done and the input should have the focus back.
func (c *Controls) Update(msg tea.KeyMsg) (chosen *controlOption, closed bool) {
	if c.open {
		return c.listKey(msg)
	}
	switch msg.String() {
	case "esc":
		return nil, true
	case "left", "shift+tab":
		c.move(-1)
	case "right", "tab":
		c.move(1)
	case "enter", "down", " ":
		c.openList()
	case "up":
		return nil, true // back up out of the row, the way you came in
	}
	return nil, false
}

// listKey is the same row of keys once a list is open, where they move through
// values rather than between buttons.
func (c *Controls) listKey(msg tea.KeyMsg) (*controlOption, bool) {
	opts := c.chips[c.idx].options
	switch msg.String() {
	case "esc":
		c.open = false // back to the buttons, not out of them
	case "up":
		c.opt = (c.opt - 1 + len(opts)) % len(opts)
	case "down":
		c.opt = (c.opt + 1) % len(opts)
	case "left", "shift+tab":
		c.open = false
		c.move(-1)
		c.openList()
	case "right", "tab":
		c.open = false
		c.move(1)
		c.openList()
	case "enter", " ":
		return c.pick(c.opt), false
	default:
		// The rows are numbered, so a number picks one — the way the CLIs' own
		// pickers work, and quicker than arrowing down to the fifth.
		if n := digit(msg); n >= 1 && n <= len(opts) {
			c.opt = n - 1
			return c.pick(c.opt), false
		}
	}
	return nil, false
}

// digit reads a plain number key, or 0 for anything that isn't one.
func digit(msg tea.KeyMsg) int {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 || msg.Alt {
		return 0
	}
	if r := msg.Runes[0]; r >= '1' && r <= '9' {
		return int(r - '0')
	}
	return 0
}

func (c *Controls) move(delta int) {
	if n := len(c.chips); n > 0 {
		c.idx = ((c.idx+delta)%n + n) % n
	}
}

// openList shows the highlighted button's values, starting on the current one.
func (c *Controls) openList() {
	c.open, c.opt = true, 0
	for i, o := range c.chips[c.idx].options {
		if o.on {
			c.opt = i
		}
	}
}

// pick applies a value to the chip and closes its list.
func (c *Controls) pick(i int) *controlOption {
	chip := &c.chips[c.idx]
	if i < 0 || i >= len(chip.options) {
		return nil
	}
	for j := range chip.options {
		chip.options[j].on = j == i
	}
	chip.value = chip.options[i].label
	c.open = false
	c.layout() // the value is part of the button, so its width just changed
	return &chip.options[i]
}

// Height is how many rows the whole thing takes: the button row, plus the list
// when one is open.
func (c *Controls) Height() int {
	if !c.open {
		return 1
	}
	return len(c.chips[c.idx].options) + 3 + 1 // box border, rows, hint, buttons
}

// ClickRow maps a row of the overlay to what was clicked. The button row is
// the last one; x picks which button.
func (c *Controls) ClickRow(row, x int) (chosen *controlOption, hit bool) {
	if row == c.Height()-1 {
		for i, chip := range c.chips {
			if x >= chip.col && x < chip.col+chip.width {
				if c.idx == i && c.open {
					c.open = false // clicking the open one shuts it
					return nil, true
				}
				c.idx = i
				c.openList()
				return nil, true
			}
		}
		return nil, false
	}
	if c.open && row >= 1 && row <= len(c.chips[c.idx].options) {
		c.opt = row - 1
		return c.pick(c.opt), true
	}
	return nil, false
}

// View draws the list, if open, above the button row.
func (c *Controls) View(w int) string {
	buttons := c.buttonRow(w)
	if !c.open {
		return buttons
	}
	chip := c.chips[c.idx]
	rows := chip.rows()
	hint := chip.label + " · ↑↓ move · 1-" + itoa(len(rows)) + " pick · enter apply · esc back"
	return renderDropUp(rows, c.opt, hint, w) + "\n" + buttons
}

// rows lays the values out the way the CLIs' own pickers do: numbered, the one
// in force ticked, and the descriptions in a column of their own so they can be
// read down rather than hunted for.
func (c *controlChip) rows() []string {
	name := 0
	for _, o := range c.options {
		name = max(name, lipgloss.Width(o.label))
	}
	out := make([]string, len(c.options))
	for i, o := range c.options {
		mark := " "
		if o.on {
			mark = "✓"
		}
		out[i] = fmt.Sprintf("%d. %s %-*s", i+1, mark, name, o.label)
		if o.desc != "" {
			out[i] += "  " + o.desc
		}
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

// buttonRow draws the chips and records where each landed, so a click can find
// them without the layout being described twice.
func (c *Controls) buttonRow(w int) string {
	var b strings.Builder
	b.WriteString(base().Render(" "))
	col := 1
	for i := range c.chips {
		style := base().Foreground(T.Lilac)
		if i == c.idx {
			style = lipgloss.NewStyle().Background(T.Purple).Foreground(T.Surface).Bold(true)
		}
		b.WriteString(style.Render(c.chipText(i)))
		b.WriteString(base().Render(" "))
		col += c.chips[i].width + 1
	}
	hint := "←→ move · enter open · esc close"
	if c.open {
		hint = "↑↓ move · enter apply · esc back"
	}
	if pad := w - col - lipgloss.Width(hint) - 1; pad > 0 {
		b.WriteString(base().Render(strings.Repeat(" ", pad)))
		b.WriteString(fg(T.Muted).Render(hint + " "))
	} else if pad := w - col; pad > 0 {
		b.WriteString(base().Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}
