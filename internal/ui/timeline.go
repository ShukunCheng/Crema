package ui

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

type Block struct {
	Kind   BlockKind
	Name   string
	Text   string
	IsErr  bool
	Result *agent.TurnResult
	// Collapsed hides a block's body behind a one-line, explicitly counted
	// summary. Only ever set by the user clicking its header — crema still
	// renders everything expanded by default.
	Collapsed bool
}

// collapsible reports whether a block kind can be folded by clicking it. Only
// the bulky ones; prose and stats always stay visible.
func collapsible(k BlockKind) bool {
	switch k {
	case BlockTool, BlockToolOutput, BlockThinking, BlockError:
		return true
	}
	return false
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
}

func NewTimeline(w, h int) *Timeline {
	vp := viewport.New(max(1, w), max(1, h))
	return &Timeline{vp: vp, width: max(1, w), follow: true}
}

func (t *Timeline) SetSize(w, h int) {
	w, h = max(1, w), max(1, h)
	if w != t.width {
		t.width = w
		t.rendered = t.rendered[:0]
		for _, b := range t.blocks {
			t.rendered = append(t.rendered, renderBlock(b, t.width))
		}
	}
	t.vp.Width, t.vp.Height = w, h
	t.sync()
}

func (t *Timeline) Len() int { return len(t.blocks) }

// Invalidate re-renders every block. Needed after a theme change, since the
// rendered text carries the old palette's escape codes.
func (t *Timeline) Invalidate() {
	t.rendered = t.rendered[:0]
	for _, b := range t.blocks {
		t.rendered = append(t.rendered, renderBlock(b, t.width))
	}
	t.sync()
}

func (t *Timeline) Following() bool { return t.follow }

func (t *Timeline) Append(b Block) {
	t.blocks = append(t.blocks, b)
	t.rendered = append(t.rendered, renderBlock(b, t.width))
	t.sync()
}

func (t *Timeline) AppendEvent(ev agent.Event) {
	switch ev.Kind {
	case agent.KindText:
		if ev.Thinking {
			t.Append(Block{Kind: BlockThinking, Text: ev.Text})
			return
		}
		t.Append(Block{Kind: BlockAssistant, Text: ev.Text})
	case agent.KindToolCall:
		if ev.Tool != nil {
			t.Append(Block{Kind: BlockTool, Name: ev.Tool.Name, Text: ev.Tool.Input})
		}
	case agent.KindToolOutput:
		if ev.Output != nil {
			t.Append(Block{Kind: BlockToolOutput, Text: ev.Output.Content, IsErr: ev.Output.IsError})
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

func (t *Timeline) Content() string { return strings.Join(t.rendered, "") }

func (t *Timeline) sync() {
	t.vp.SetContent(t.Content())
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
		if line == contentLine {
			if collapsible(t.blocks[i].Kind) {
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

// ToggleCollapse folds or unfolds one block.
func (t *Timeline) ToggleCollapse(i int) {
	if i < 0 || i >= len(t.blocks) {
		return
	}
	t.blocks[i].Collapsed = !t.blocks[i].Collapsed
	t.rendered[i] = renderBlock(t.blocks[i], t.width)
	t.vp.SetContent(t.Content())
}

func renderBlock(b Block, w int) string {
	if b.Collapsed && collapsible(b.Kind) {
		return renderCollapsed(b, w)
	}
	return renderExpanded(b, w)
}

// renderCollapsed states exactly how much is hidden and how to get it back.
func renderCollapsed(b Block, w int) string {
	full := strings.TrimRight(renderExpanded(b, w), "\n")
	hidden := strings.Count(full, "\n") + 1
	head := "output"
	switch b.Kind {
	case BlockTool:
		head = "⏵ " + b.Name
	case BlockThinking:
		head = "✳ thinking"
	case BlockError:
		head = "✖ error"
	}
	label := fmt.Sprintf("▸ %s — %d lines hidden, click to expand", head, hidden)
	return lipgloss.NewStyle().Foreground(T.Yellow).Width(max(1, w)).Render(label) + "\n"
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
