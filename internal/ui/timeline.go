package ui

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type BlockKind int

const (
	BlockUser BlockKind = iota
	BlockAssistant
	BlockThinking
	BlockTool
	BlockToolOutput
	BlockError
	BlockSystem
	BlockStats
)

// Block is one entry in a conversation. The json tags are what gets written to
// the state file, so renaming a field changes the on-disk format.
type Block struct {
	Kind   BlockKind         `json:"kind"`
	Name   string            `json:"name,omitempty"`
	Text   string            `json:"text,omitempty"`
	IsErr  bool              `json:"is_err,omitempty"`
	Result *agent.TurnResult `json:"result,omitempty"`
	// Collapsed hides a block's body behind a one-line, explicitly counted
	// summary. Only ever set by the user clicking its header — crema still
	// renders everything expanded by default.
	Collapsed bool `json:"collapsed,omitempty"`
	// Sub marks work a subagent did rather than the main conversation. Sub
	// blocks fold into their own labeled runs, prose included: a subagent's
	// narration is an aside, not the answer.
	Sub bool `json:"sub,omitempty"`
}

// fileChangeTools are the tools that edit the working tree. A turn is judged
// by what it changed, so those arrive open while everything else arrives
// folded — the shell commands, the searches, the reading, the thinking. Names
// are the two CLIs' own: Claude Code writes files with Edit and Write, codex
// with apply_patch.
var fileChangeTools = map[string]bool{
	"edit": true, "multiedit": true, "write": true, "notebookedit": true,
	"apply_patch": true, "applypatch": true, "str_replace_editor": true,
}

func changesFiles(tool string) bool { return fileChangeTools[strings.ToLower(tool)] }

// collapsible reports whether a block kind can be folded by clicking it. Only
// the bulky ones; prose and stats always stay visible.
func collapsible(k BlockKind) bool {
	switch k {
	case BlockTool, BlockToolOutput, BlockThinking, BlockError:
		return true
	}
	return false
}

// folded is a block that is currently standing behind a summary line. A
// subagent's prose can fold too — for the main conversation only bulky kinds
// can, so the answer itself can never disappear behind a click.
func folded(b Block) bool { return b.Collapsed && (collapsible(b.Kind) || b.Sub) }

// foldRun is the half-open range of folded blocks around i — a call, its
// output, the next call, and so on all fold into one line together. A block
// that isn't folded gets the empty range i,i.
//
// Runs are what the CLIs' own transcripts show: a turn that ran three commands
// is three calls and three outputs, six lines of "▸ …" saying nothing. One
// line saying "Ran 3 shell commands" is the same information.
func foldRun(blocks []Block, i int) (from, to int) {
	if i < 0 || i >= len(blocks) || !folded(blocks[i]) {
		return i, i
	}
	from, to = i, i+1
	// A subagent's run and the main turn's never merge: the summary line says
	// whose work it is, so it can only stand for one of them.
	for from > 0 && folded(blocks[from-1]) && blocks[from-1].Sub == blocks[i].Sub {
		from--
	}
	for to < len(blocks) && folded(blocks[to]) && blocks[to].Sub == blocks[i].Sub {
		to++
	}
	return from, to
}

// Timeline owns the conversation blocks and their scroll state. Rendered text
// is cached per block and invalidated only when the width changes, so a long
// session doesn't re-render everything on every event.
type Timeline struct {
	blocks   []Block
	rendered []string
	vp       viewport.Model
	width    int
	follow   bool
	// status is the working line drawn while a turn runs, and pending are the
	// messages typed during it — both after the conversation, in the order
	// they are going to happen.
	status  string
	pending []string

	// Selection lives here rather than in the terminal: mouse reporting is
	// all-or-nothing for the whole window, so the only way to let a plain drag
	// select in this pane while the rest stays clickable is to draw it here.
	sel selection
}

func NewTimeline(w, h int) *Timeline {
	vp := viewport.New(max(1, w), max(1, h))
	vp.Style = base() // paint the theme behind the empty rows too
	return &Timeline{vp: vp, width: max(1, w), follow: true}
}

func (t *Timeline) SetSize(w, h int) {
	w, h = max(1, w), max(1, h)
	if w != t.width {
		t.width = w
		t.renderAll()
	}
	t.vp.Width, t.vp.Height = w, h
	t.sync()
}

// renderAt draws one block. A folded block is part of a run: only the first of
// the run draws anything, and it draws the whole run's summary. The others
// render to nothing, which keeps rendered index-for-index with blocks — the
// line arithmetic in HeaderBlockAt and the selection both depend on that.
func (t *Timeline) renderAt(i int) string {
	from, to := foldRun(t.blocks, i)
	switch {
	case from == to:
		return renderExpanded(t.blocks[i], t.width)
	case i > from:
		return ""
	}
	return renderCollapsed(t.blocks[from:to], t.width)
}

func (t *Timeline) renderAll() {
	t.rendered = append(t.rendered[:0], make([]string, len(t.blocks))...)
	for i := range t.blocks {
		t.rendered[i] = t.renderAt(i)
	}
}

func (t *Timeline) Len() int { return len(t.blocks) }

// LastText is what the agent said last: the final block of prose, skipping
// past the stats line a turn ends with and anything that isn't the agent
// talking. Empty when the turn ended on something else entirely.
func (t *Timeline) LastText() string {
	for i := len(t.blocks) - 1; i >= 0; i-- {
		switch b := t.blocks[i]; b.Kind {
		case BlockAssistant:
			return b.Text
		case BlockStats:
			continue // the turn's own footer sits after the reply
		default:
			return ""
		}
	}
	return ""
}

// Invalidate re-renders every block. Needed after a theme change, since the
// rendered text carries the old palette's escape codes.
func (t *Timeline) Invalidate() {
	t.vp.Style = base()
	t.renderAll()
	t.sync()
}

func (t *Timeline) Following() bool { return t.follow }

// GotoEnd jumps to the newest entry and follows from there. Scrolling back
// through a conversation stops the pane chasing new output, which is right
// while you are reading — but sending a message ends the reading: what you
// want to see next is the answer to it, at the bottom.
func (t *Timeline) GotoEnd() {
	t.follow = true
	t.vp.GotoBottom()
}

// Blocks exposes the conversation for saving.
func (t *Timeline) Blocks() []Block { return t.blocks }

// Restore replaces the conversation with previously saved blocks.
func (t *Timeline) Restore(blocks []Block) {
	t.blocks = append(t.blocks[:0], blocks...)
	t.renderAll()
	t.follow = true
	t.sync()
}

// Append adds a block and redraws the run it lands in — a folded block joining
// the run above it changes that run's summary, and nothing else on screen.
func (t *Timeline) Append(b Block) {
	t.blocks = append(t.blocks, b)
	t.rendered = append(t.rendered, "")
	last := len(t.blocks) - 1
	from, _ := foldRun(t.blocks, last)
	// Either this block draws its own line, or it joined the run above and
	// only that run's summary needs saying again.
	t.rendered[min(from, last)] = t.renderAt(min(from, last))
	t.sync()
}

func (t *Timeline) AppendEvent(ev agent.Event) {
	sub := ev.SubID != ""
	switch ev.Kind {
	case agent.KindText:
		if ev.Thinking {
			t.Append(Block{Kind: BlockThinking, Text: ev.Text, Collapsed: true, Sub: sub})
			return
		}
		// A subagent's prose arrives folded: it reports to the model, not to
		// the user, and the main conversation carries what came of it. It is
		// all still there, one click away.
		t.Append(Block{Kind: BlockAssistant, Text: ev.Text, Collapsed: sub, Sub: sub})
	case agent.KindToolCall:
		if ev.Tool != nil {
			t.Append(Block{
				Kind: BlockTool, Name: ev.Tool.Name, Text: ev.Tool.Input, Sub: sub,
				// A file edit arrives open whoever made it — a turn is judged
				// by what it changed.
				Collapsed: !changesFiles(ev.Tool.Name),
			})
		}
	case agent.KindToolOutput:
		if ev.Output != nil {
			t.Append(Block{
				Kind: BlockToolOutput, Text: ev.Output.Content, IsErr: ev.Output.IsError, Sub: sub,
				// A failure is the one output worth reading unprompted.
				Collapsed: !ev.Output.IsError,
			})
		}
	case agent.KindError:
		t.Append(Block{Kind: BlockError, Text: ev.Text})
	case agent.KindTurnEnd:
		if ev.Result != nil && ev.Result.Err != "" {
			t.Append(Block{Kind: BlockError, Text: ev.Result.Err})
		}
		t.Append(Block{Kind: BlockStats, Result: ev.Result})
	}
}

// Content is the conversation, with anything still waiting to be sent drawn
// after it. A queued message belongs at the end of the transcript because that
// is where it is going to land — it is the next thing in the conversation, not
// a box floating over one.
func (t *Timeline) Content() string {
	body := strings.Join(t.rendered, "") + t.status
	for _, p := range t.pending {
		body += renderPending(p, t.width)
	}
	return body
}

// SetStatus replaces the working line drawn under the conversation, empty when
// the agent is idle. It arrives once per frame, so an unchanged line does
// nothing rather than re-splitting the whole transcript.
func (t *Timeline) SetStatus(line string) {
	if line == t.status {
		return
	}
	t.status = line
	t.sync()
}

// statusRows is 1 while the working line is up, and the offset between the
// conversation and the messages waiting after it.
func (t *Timeline) statusRows() int {
	if t.status == "" {
		return 0
	}
	return 1
}

// SetPending replaces the waiting messages drawn under the conversation.
func (t *Timeline) SetPending(msgs []string) {
	if len(msgs) == len(t.pending) {
		same := true
		for i := range msgs {
			same = same && msgs[i] == t.pending[i]
		}
		if same {
			return // redrawing on every keystroke would fight the scroll
		}
	}
	t.pending = append(t.pending[:0], msgs...)
	t.sync()
}

// blockLines is how many lines the conversation itself takes, which is where
// the waiting messages start.
func (t *Timeline) blockLines() (n int) {
	for _, r := range t.rendered {
		n += strings.Count(r, "\n")
	}
	return n
}

// PendingAt maps a content line to the waiting message drawn on it, or -1.
func (t *Timeline) PendingAt(contentLine int) int {
	i := contentLine - t.blockLines() - t.statusRows()
	if i < 0 || i >= len(t.pending) {
		return -1
	}
	return i
}

func (t *Timeline) sync() {
	t.vp.SetContent(t.highlighted())
	if t.follow {
		t.vp.GotoBottom()
	}
}

func (t *Timeline) Update(msg tea.Msg) tea.Cmd {
	// The viewport's keymap has no home/end bindings, so handle them here.
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "home":
			t.vp.GotoTop()
			t.follow = t.vp.AtBottom()
			return nil
		case "end":
			t.vp.GotoBottom()
			t.follow = true
			return nil
		}
	}
	var cmd tea.Cmd
	t.vp, cmd = t.vp.Update(msg)
	t.follow = t.vp.AtBottom()
	return cmd
}

func (t *Timeline) View() string { return t.vp.View() }

// YOffset is the first content line currently visible, for hit-testing.
func (t *Timeline) YOffset() int { return t.vp.YOffset }

// HeaderBlockAt returns the index of the collapsible block whose header sits on
// contentLine, or -1. Only headers toggle, so clicking a block's body scrolls
// or focuses instead of folding it out from under the pointer.
func (t *Timeline) HeaderBlockAt(contentLine int) int {
	line := 0
	for i, r := range t.rendered {
		if r == "" {
			continue // folded into the run above; it has no line of its own
		}
		if line == contentLine {
			if b := t.blocks[i]; collapsible(b.Kind) || b.Sub {
				return i
			}
			return -1
		}
		line += strings.Count(r, "\n")
		if line > contentLine {
			return -1
		}
	}
	return -1
}

// ToggleCollapse opens or closes what the clicked line stands for. A summary
// line stands for its whole run, so clicking it opens all of it — folding them
// one at a time to get here would be a strange way to have arrived, and
// opening one of six identical hidden blocks isn't what the click meant.
func (t *Timeline) ToggleCollapse(i int) {
	if i < 0 || i >= len(t.blocks) {
		return
	}
	from, to := foldRun(t.blocks, i)
	if from == to {
		t.blocks[i].Collapsed = true
	}
	for j := from; j < to; j++ {
		t.blocks[j].Collapsed = false
	}
	t.renderAll()
	t.vp.SetContent(t.highlighted())
}

// blockTitle is what a collapsible block calls itself, with no glyph of its
// own: the expander in front of it is the only marker, so a folded tool reads
// "▸ Bash" rather than stacking two triangles.
func blockTitle(b Block) string {
	switch b.Kind {
	case BlockTool:
		return b.Name
	case BlockThinking:
		return "thinking"
	case BlockError:
		return "error"
	}
	return "output"
}

// renderCollapsed is the one-line stand-in for a folded run. Grey, because it
// is the one line on screen that is deliberately not worth reading — the ▸ is
// there to say the rest is one click away.
func renderCollapsed(run []Block, w int) string {
	label := "▸ "
	if len(run) > 0 && run[0].Sub {
		label = "▸ subagent · "
	}
	return fg(T.Muted).Width(max(1, w)).Render(label+summarize(run)) + "\n"
}

// toolPhrase maps a tool to the sentence the CLIs use for it, keyed by the
// lower-cased name both backends report. A tool that isn't here is named
// instead of described, which is better than describing it wrongly.
var toolPhrase = []struct {
	tools []string
	one   string // "%d" is filled with the count
	many  string
}{
	{[]string{"bash", "shell", "run_command", "execute_command", "local_shell"},
		"Ran %d shell command", "Ran %d shell commands"},
	{[]string{"read", "view", "read_file", "notebookread"},
		"Read %d file", "Read %d files"},
	{[]string{"grep", "glob", "search", "find", "codebase_search", "ripgrep"},
		"Searched %d time", "Searched %d times"},
	{[]string{"edit", "multiedit", "write", "notebookedit", "apply_patch", "applypatch", "str_replace_editor"},
		"Changed %d file", "Changed %d files"},
	{[]string{"webfetch", "websearch", "web_search", "fetch"},
		"Fetched %d page", "Fetched %d pages"},
	{[]string{"task", "agent"}, "Ran %d agent", "Ran %d agents"},
	{[]string{"todowrite", "update_plan"}, "Updated the plan", "Updated the plan %d times"},
}

// phraseOf finds the bucket a tool belongs to, or -1.
func phraseOf(tool string) int {
	tool = strings.ToLower(tool)
	for i, p := range toolPhrase {
		for _, t := range p.tools {
			if t == tool {
				return i
			}
		}
	}
	return -1
}

// count renders one bucket: "Ran 1 shell command", "Read 3 files".
func count(one, many string, n int) string {
	f := many
	if n == 1 {
		f = one
	}
	if !strings.Contains(f, "%d") {
		return f
	}
	return fmt.Sprintf(f, n)
}

// summarize says what a folded run did. Outputs are not counted — an output
// belongs to the call above it, and "Ran 2 shell commands" already implies two
// of them. A run of nothing but output says so, since that is all it is.
func summarize(run []Block) string {
	buckets := make([]int, len(toolPhrase))
	other := map[string]int{}
	var names []string // distinct unrecognised tools, in the order they ran
	thinks, errs, outs, prose := 0, 0, 0, 0
	for _, b := range run {
		switch b.Kind {
		case BlockAssistant:
			prose++ // only a subagent's prose ever folds
		case BlockTool:
			if i := phraseOf(b.Name); i >= 0 {
				buckets[i]++
				continue
			}
			if other[b.Name]++; other[b.Name] == 1 {
				names = append(names, b.Name)
			}
		case BlockThinking:
			thinks++
		case BlockError:
			errs++
		case BlockToolOutput:
			outs++
		}
	}

	var parts []string
	for i, n := range buckets {
		if n > 0 {
			parts = append(parts, count(toolPhrase[i].one, toolPhrase[i].many, n))
		}
	}
	switch {
	case len(names) == 1:
		parts = append(parts, count("Used "+names[0], "Used "+names[0]+" %d times", other[names[0]]))
	case len(names) > 1:
		parts = append(parts, fmt.Sprintf("Used %d tools", len(names)))
	}
	if thinks > 0 {
		parts = append(parts, count("Thought once", "Thought %d times", thinks))
	}
	if prose > 0 {
		parts = append(parts, count("Reported back", "Reported back %d times", prose))
	}
	if errs > 0 {
		parts = append(parts, count("%d error", "%d errors", errs))
	}
	if len(parts) == 0 {
		return count("%d output", "%d outputs", max(outs, 1))
	}
	return strings.Join(parts, " · ")
}

func renderExpanded(b Block, w int) string {
	switch b.Kind {
	case BlockUser:
		return RenderUser(b.Text, w)
	case BlockThinking:
		return RenderThinking(b.Text, w)
	case BlockTool:
		return RenderTool(b.Name, b.Text, w)
	case BlockToolOutput:
		return RenderToolOutput(b.Text, b.IsErr, w)
	case BlockError:
		return RenderError(b.Text, w)
	case BlockSystem:
		return RenderSystem(b.Text, w)
	case BlockStats:
		return RenderStats(b.Result, w)
	default:
		return RenderAssistant(b.Text, w)
	}
}
