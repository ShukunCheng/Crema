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
	// controlMain and controlAgent are the roster's rows — the agent itself
	// and each subagent — when few enough are running to be listed. Enter on
	// a subagent addresses the next message to it; x stops it.
	controlMain controlKind = iota
	controlAgent
	controlModel
	controlPermission
	// controlEffort is how hard the backend is asked to think. Offered only by
	// a backend that has the setting, and it is the one value here that cannot
	// be changed under a live process — see Session.SetEffort.
	controlEffort
	// controlBackground is the counts alone — "6 agents · 2 shells" — for
	// when too much is running to list, and the walk's one stop for it: enter
	// opens the list, where enter views a task and x stops it.
	controlBackground
	// controlStop is not a button but an answer: the walk asked for a task to
	// be stopped.
	controlStop
)

// controlOption is one value a chip can take.
type controlOption struct {
	kind        controlKind
	label, desc string
	perm        agent.PermissionMode
	model       string
	effort      string
	task        string // controlBackground: the CLI's own task id
	on          bool
}

// controlChip is one button: what it sets, what it is set to, and what else it
// could be set to. A roster row is a chip too, with a task and no options.
type controlChip struct {
	kind    controlKind
	label   string
	value   string
	task    string // controlAgent: the CLI's id for the subagent
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

	// Effort is only on the row when the backend has such a setting, so a
	// Codex or mock agent is not offered a dial it does not have.
	var effortChip *controlChip
	if levels := s.efforts(); len(levels) > 1 {
		chip := controlChip{kind: controlEffort, label: "effort", value: effortLabel(s.Effort)}
		describer, _ := s.Backend.(agent.EffortChooser)
		for _, e := range levels {
			desc := ""
			if describer != nil {
				desc = describer.DescribeEffort(e)
			}
			chip.options = append(chip.options, controlOption{
				kind: controlEffort, label: effortLabel(e), desc: desc,
				effort: e, on: e == s.Effort,
			})
		}
		effortChip = &chip
	}

	// The hub's own two values come first, always in the same two places:
	// they are what the walk is mostly for, and they are there whether
	// anything is running or not. A row that moves depending on what a
	// subagent happens to be doing is a row you have to look for.
	c.chips = append(c.chips, model, perm)
	if effortChip != nil {
		c.chips = append(c.chips, *effortChip)
	}
	// Then the running work, in the two cases the CLI has. Few enough to
	// list: a stop for each of the roster's rows, in the order they are
	// drawn. Too many: the counts are one stop, and enter opens the list.
	if s.rosterShown() {
		for _, r := range s.roster() {
			kind := controlAgent
			if r.main {
				kind = controlMain
			}
			c.chips = append(c.chips, controlChip{kind: kind, label: r.name, value: r.what, task: r.task})
		}
	} else if bg, ok := backgroundChip(s); ok {
		c.chips = append(c.chips, bg)
	}
	c.layout()
	return c
}

// backgroundChip is the button for what is running behind the turn, for the
// case where there is too much of it to list. Picking a row shows that task's
// own output; x on a row stops it, which the CLI's stdin protocol turned out
// to support (stop_task) after all.
func backgroundChip(s *Session) (controlChip, bool) {
	shells, agents := s.Background()
	if shells+agents == 0 {
		return controlChip{}, false
	}
	chip := controlChip{
		kind:  controlBackground,
		label: "background",
		value: backgroundLabel(shells, agents),
	}
	for _, t := range s.tasks {
		if t.Status != "running" {
			continue
		}
		kind := t.Type
		if kind == "local_bash" {
			kind = "shell"
		}
		desc := t.Desc
		if t.LastTool != "" {
			desc += " · now: " + t.LastTool
		}
		if t.Tokens > 0 {
			desc += " · " + shortCount(t.Tokens) + " tokens"
		}
		chip.options = append(chip.options, controlOption{
			kind: controlBackground, label: kind, desc: desc, task: t.ID,
		})
	}
	return chip, true
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

// effortLabel is what an effort level is called on the row. The CLI's own
// default has no name of its own, so it borrows the word every other value
// here uses for the same idea.
func effortLabel(e string) string {
	if e == agent.DefaultEffort {
		return "default"
	}
	return e
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

// Wants reports whether a key is the walk's to handle rather than the
// draft's. Movement and enter always are. A digit is, while a numbered list
// is open — but not while only the highlight shows, where a bare digit is the
// start of a message. And x is, when it means stop: on a subagent's row, or
// in the background list. Everything else is typing, and typing ends the
// walk and goes into the box.
func (c *Controls) Wants(msg tea.KeyMsg) bool {
	if !isTyping(msg) {
		return true
	}
	if c.open && digit(msg) > 0 {
		return true
	}
	if msg.String() == "x" {
		if c.idx >= 0 && c.idx < len(c.chips) {
			kind := c.chips[c.idx].kind
			return kind == controlAgent || (c.open && kind == controlBackground)
		}
	}
	return false
}

// Update handles a key. chosen is a value to apply; closed means the row is
// done and the input should have the focus back.
func (c *Controls) Update(msg tea.KeyMsg) (chosen *controlOption, closed bool) {
	if c.open {
		return c.listKey(msg)
	}
	switch msg.String() {
	case "esc":
		return nil, true
	case "up", "left", "shift+tab":
		// A walk, not a carousel: ↓ came from the input box, so ↑ off the
		// first value goes back to it. Wrapping around to the far end would
		// be a surprising place to land.
		if c.idx == 0 {
			return nil, true
		}
		c.idx--
	case "down", "right", "tab":
		if c.idx < len(c.chips)-1 {
			c.idx++
		}
		// At the last value ↓ does nothing. There is nothing past it — the
		// background button is not even on the row unless something is
		// running — and dropping back into the input box would send the next
		// keystroke somewhere the eye is not.
	case "enter", " ":
		switch chip := c.chips[c.idx]; chip.kind {
		case controlMain:
			// Talking to the agent itself is just typing. Back to the box.
			return nil, true
		case controlAgent:
			// Talk to it: the next message goes to this subagent.
			return &controlOption{kind: controlAgent, task: chip.task, label: chip.label}, false
		}
		// Only enter opens one, so walking past a value costs one key and
		// nothing opens by accident on the way through.
		c.openList()
	case "x":
		if chip := c.chips[c.idx]; chip.kind == controlAgent {
			return &controlOption{kind: controlStop, task: chip.task, label: chip.label}, false
		}
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
	case "x":
		// In the background list, x stops the highlighted task.
		if c.chips[c.idx].kind == controlBackground && c.opt < len(opts) {
			o := opts[c.opt]
			return &controlOption{kind: controlStop, task: o.task, label: o.label}, false
		}
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

// move steps the highlight within the row, clamped at both ends. Only the
// open list wraps; the row itself is a walk out from the input box and back.
func (c *Controls) move(delta int) {
	c.idx = max(0, min(c.idx+delta, len(c.chips)-1))
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

// Height is how many rows float above the input: none. What ↓ selects is
// highlighted in the status bar, where the model and the mode are already
// written — a strip of buttons over the conversation would only say the same
// words a second time. The open list still takes the status bar's place,
// where a picker has room to spread out.
func (c *Controls) Height() int { return 0 }

// ListHeight is how tall that bottom strip is while a list is open: a title,
// the values, and the key hint.
func (c *Controls) ListHeight() int {
	return len(c.chips[c.idx].options) + 2
}

// Kind is what the highlighted button changes, so the status bar can show
// which of its own values is selected.
func (c *Controls) Kind() controlKind {
	if c.idx < 0 || c.idx >= len(c.chips) {
		return controlModel
	}
	return c.chips[c.idx].kind
}

// Task is the subagent the walk is on, "" when it is on something else.
func (c *Controls) Task() string {
	if c.idx < 0 || c.idx >= len(c.chips) {
		return ""
	}
	return c.chips[c.idx].task
}

// SelectRosterRow points the walk at one of the roster's rows by position —
// what a click on that row means. The rows are the chips after the hub's own
// values, in the order they are drawn, so the offset is where they start
// rather than zero.
func (c *Controls) SelectRosterRow(i int) bool {
	if i < 0 {
		return false
	}
	return c.SelectIndex(c.rosterFrom() + i)
}

// rosterFrom is the index of the first roster row among the chips.
func (c *Controls) rosterFrom() int {
	for i, chip := range c.chips {
		if chip.kind == controlMain || chip.kind == controlAgent {
			return i
		}
	}
	return len(c.chips)
}

// SelectIndex points the walk at one chip by position.
func (c *Controls) SelectIndex(i int) bool {
	if i < 0 || i >= len(c.chips) {
		return false
	}
	c.idx, c.open = i, false
	return true
}

// SelectKind points the row at one particular button and opens it — what a
// click on that value in the status bar means.
func (c *Controls) SelectKind(kind controlKind) bool {
	for i, chip := range c.chips {
		if chip.kind != kind {
			continue
		}
		if c.idx == i && c.open {
			c.open = false // clicking the open one shuts it
			return true
		}
		c.idx = i
		c.openList()
		return true
	}
	return false
}

// ClickListRow maps a click inside the bottom strip to a value. Row 0 is the
// title and the last row is the hint; neither picks anything.
func (c *Controls) ClickListRow(row, _ int) (chosen *controlOption, hit bool) {
	if !c.open || row < 1 || row > len(c.chips[c.idx].options) {
		return nil, false
	}
	c.opt = row - 1
	return c.pick(c.opt), true
}

// View is what floats above the input: nothing. The selection lives in the
// status bar and the open list replaces it.
func (c *Controls) View(int) string { return "" }

// ListView draws the open button's values where the status bar was: a title,
// the numbered rows with the one in force ticked, and the keys. The status
// bar stands aside while a choice is being made — choosing is the one thing
// happening, and the picker gets the room the CLIs' own pickers get.
func (c *Controls) ListView(w int) string {
	chip := c.chips[c.idx]
	rows := chip.rows()
	out := []string{fg(T.Magenta).Bold(true).Width(max(1, w)).Render(" " + strings.ToUpper(chip.label))}
	for i, r := range rows {
		st := base().Foreground(T.Fg).Width(max(1, w))
		if i == c.opt {
			st = lipgloss.NewStyle().Background(T.Purple).Foreground(T.Surface).Bold(true).Width(max(1, w))
		}
		out = append(out, st.Render(" "+r))
	}
	verb := "enter apply"
	if chip.kind == controlBackground {
		verb = "enter view · x stop"
	}
	hint := "↑↓ move · 1-" + itoa(len(rows)) + " pick · " + verb + " · esc back"
	out = append(out, fg(T.Muted).Width(max(1, w)).Render(" "+hint))
	return strings.Join(out, "\n")
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
