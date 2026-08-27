package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAppendEventMapsEveryKind(t *testing.T) {
	tl := NewTimeline(60, 10)
	tl.AppendEvent(agent.Event{Kind: agent.KindText, Text: "hello"})
	tl.AppendEvent(agent.Event{Kind: agent.KindText, Thinking: true, Text: "hmm"})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall, Tool: &agent.ToolCall{Name: "Bash", Input: "ls -la"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolOutput, Output: &agent.ToolOutput{Content: "a.txt"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindError, Text: "went wrong"})
	tl.AppendEvent(agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{DurationMS: 1000}})
	if tl.Len() != 6 {
		t.Fatalf("Len = %d, want 6", tl.Len())
	}
	// Prose and errors show; the noisy kinds arrive folded, and a run of them
	// leaves one summary line behind.
	c := tl.Content()
	for _, want := range []string{"hello", "Ran 1 shell command · Thought once", "went wrong"} {
		if !strings.Contains(c, want) {
			t.Fatalf("content missing %q:\n%s", want, c)
		}
	}
	for _, hidden := range []string{"hmm", "ls -la", "a.txt"} {
		if strings.Contains(c, hidden) {
			t.Fatalf("content should have folded %q away:\n%s", hidden, c)
		}
	}
	// Unfolding a block shows what it was hiding.
	for i, b := range tl.Blocks() {
		if b.Kind == BlockTool {
			tl.ToggleCollapse(i)
		}
	}
	if !strings.Contains(tl.Content(), "ls -la") {
		t.Fatalf("unfolding the tool call must show it:\n%s", tl.Content())
	}
}

// One triangle per line, pointing the way it folds: ▾ open, ▸ folded. The kind
// used to carry a glyph of its own, which stacked two triangles onto a folded
// tool.
func TestABlockCarriesOneFoldMarker(t *testing.T) {
	blocks := []Block{
		{Kind: BlockTool, Name: "Bash", Text: "go test ./..."},
		{Kind: BlockThinking, Text: "weighing it up"},
		{Kind: BlockError, Text: "it went wrong"},
	}
	for _, b := range blocks {
		name := blockTitle(b)
		open := firstLine(renderExpanded(b, 60))
		folded := firstLine(renderCollapsed([]Block{b}, 60))

		if !strings.HasPrefix(open, "▾ "+name) {
			t.Fatalf("open %s reads %q, want it to start ▾ %s", name, open, name)
		}
		if !strings.HasPrefix(folded, "▸ ") {
			t.Fatalf("folded %s reads %q, want it to start ▸", name, folded)
		}
		for _, line := range []string{open, folded} {
			if n := strings.Count(line, "▸") + strings.Count(line, "▾") +
				strings.Count(line, "▶"); n != 1 {
				t.Fatalf("%q carries %d triangles, want exactly one", line, n)
			}
		}
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimRight(s, "\n"), "\n")
	return strings.TrimRight(stripSGR(line), " ")
}

// What a turn changed is what a turn is judged by, so a file edit arrives
// open while everything around it arrives folded.
func TestFileChangesArriveExpandedAndTheRestFolded(t *testing.T) {
	tl := NewTimeline(70, 10)
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall,
		Tool: &agent.ToolCall{Name: "Edit", Input: "internal/ui/app.go: one line"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall,
		Tool: &agent.ToolCall{Name: "apply_patch", Input: "*** Update File: main.go"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall,
		Tool: &agent.ToolCall{Name: "Bash", Input: "go test ./..."}})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall,
		Tool: &agent.ToolCall{Name: "Read", Input: "internal/ui/app.go"}})

	c := tl.Content()
	for _, shown := range []string{"internal/ui/app.go: one line", "*** Update File: main.go"} {
		if !strings.Contains(c, shown) {
			t.Fatalf("a file change must arrive expanded, missing %q:\n%s", shown, c)
		}
	}
	for _, folded := range []string{"go test ./...", "internal/ui/app.go\n"} {
		if strings.Contains(c, folded) {
			t.Fatalf("%q should have arrived folded:\n%s", folded, c)
		}
	}
}

// A failed tool is the one output that shows itself.
func TestAFailedToolOutputArrivesExpanded(t *testing.T) {
	tl := NewTimeline(70, 10)
	tl.AppendEvent(agent.Event{Kind: agent.KindToolOutput,
		Output: &agent.ToolOutput{Content: "exit status 1: undefined: foo", IsError: true}})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolOutput,
		Output: &agent.ToolOutput{Content: "all good"}})

	c := tl.Content()
	if !strings.Contains(c, "undefined: foo") {
		t.Fatalf("a failure must not be hidden:\n%s", c)
	}
	if strings.Contains(c, "all good") {
		t.Fatalf("an ordinary output arrives folded:\n%s", c)
	}
}

func TestContentRerendersOnWidthChange(t *testing.T) {
	tl := NewTimeline(80, 10)
	long := strings.Repeat("word ", 40)
	tl.Append(Block{Kind: BlockAssistant, Text: long})
	wide := len(strings.Split(tl.Content(), "\n"))
	tl.SetSize(30, 10)
	narrow := len(strings.Split(tl.Content(), "\n"))
	if narrow <= wide {
		t.Fatalf("narrower width must wrap into more lines: wide=%d narrow=%d", wide, narrow)
	}
}

func TestFollowStaysAtBottomUntilUserScrollsUp(t *testing.T) {
	tl := NewTimeline(40, 5)
	for i := 0; i < 40; i++ {
		tl.Append(Block{Kind: BlockAssistant, Text: "line"})
	}
	if !tl.Following() {
		t.Fatal("should auto-follow new output")
	}
	tl.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if tl.Following() {
		t.Fatal("scrolling up must stop follow mode")
	}
	tl.Append(Block{Kind: BlockAssistant, Text: "more"})
	if tl.Following() {
		t.Fatal("appending must not yank a scrolled-back user to the bottom")
	}
	tl.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !tl.Following() {
		t.Fatal("End must resume follow mode")
	}
}

func TestViewFitsRequestedHeight(t *testing.T) {
	tl := NewTimeline(40, 6)
	for i := 0; i < 30; i++ {
		tl.Append(Block{Kind: BlockAssistant, Text: "x"})
	}
	if got := len(strings.Split(tl.View(), "\n")); got != 6 {
		t.Fatalf("View height = %d, want 6", got)
	}
}

// tlWith is a timeline holding exactly these blocks.
func tlWith(blocks ...Block) *Timeline {
	tl := NewTimeline(60, 20)
	for _, b := range blocks {
		tl.Append(b)
	}
	return tl
}

func call(name string) Block {
	return Block{Kind: BlockTool, Name: name, Text: "{}", Collapsed: true}
}

func output() Block {
	return Block{Kind: BlockToolOutput, Text: "…", Collapsed: true}
}

// A run of folded calls and their outputs is one line, not seven.
func TestAFoldedRunIsOneSummaryLine(t *testing.T) {
	tl := tlWith(call("Bash"), output(), call("Bash"), output(), call("Read"), output())
	c := strings.TrimRight(stripSGR(tl.Content()), "\n")
	if n := strings.Count(c, "\n"); n != 0 {
		t.Fatalf("six folded blocks drew %d lines:\n%s", n+1, c)
	}
	if got := strings.TrimSpace(c); got != "▸ Ran 2 shell commands · Read 1 file" {
		t.Fatalf("summary reads %q", got)
	}
}

// Prose between two runs breaks them apart, because it is on screen and they
// are not.
func TestProseSplitsARunInTwo(t *testing.T) {
	tl := tlWith(call("Bash"), output(),
		Block{Kind: BlockAssistant, Text: "now the other one"},
		call("Bash"), output())
	lines := strings.Split(strings.TrimRight(stripSGR(tl.Content()), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want summary, prose, summary:\n%s", strings.Join(lines, "\n"))
	}
	for _, i := range []int{0, 2} {
		if !strings.Contains(lines[i], "Ran 1 shell command") {
			t.Fatalf("line %d is %q", i, lines[i])
		}
	}
}

// The summary line is the click target for everything behind it.
func TestClickingASummaryOpensTheWholeRun(t *testing.T) {
	tl := tlWith(call("Bash"), output(), call("Read"), output())
	if got := tl.HeaderBlockAt(0); got != 0 {
		t.Fatalf("the summary should map to the run's first block, got %d", got)
	}
	if got := tl.HeaderBlockAt(1); got != -1 {
		t.Fatalf("there is no second line to click, got %d", got)
	}
	tl.ToggleCollapse(0)
	for i, b := range tl.Blocks() {
		if b.Collapsed {
			t.Fatalf("block %d is still folded after opening the run", i)
		}
	}
}

// Folding one open block again folds only that block.
func TestFoldingOneBlockDoesNotFoldItsNeighbours(t *testing.T) {
	tl := tlWith(Block{Kind: BlockTool, Name: "Write", Text: "{}"},
		Block{Kind: BlockTool, Name: "Edit", Text: "{}"})
	tl.ToggleCollapse(0)
	if !tl.Blocks()[0].Collapsed || tl.Blocks()[1].Collapsed {
		t.Fatalf("only the clicked block should fold: %+v", tl.Blocks())
	}
}

// What the summary says, for the shapes a turn actually takes.
func TestSummarizeNamesWhatTheRunDid(t *testing.T) {
	for _, c := range []struct {
		run  []Block
		want string
	}{
		{[]Block{call("Bash"), output()}, "Ran 1 shell command"},
		{[]Block{call("bash"), call("Bash")}, "Ran 2 shell commands"},
		{[]Block{call("Read"), call("Grep"), call("Glob")}, "Read 1 file · Searched 2 times"},
		{[]Block{call("Write"), call("Edit")}, "Changed 2 files"},
		{[]Block{call("TodoWrite")}, "Updated the plan"},
		{[]Block{call("Frobnicate")}, "Used Frobnicate"},
		{[]Block{call("Frobnicate"), call("Frobnicate")}, "Used Frobnicate 2 times"},
		{[]Block{call("Frobnicate"), call("Widget")}, "Used 2 tools"},
		{[]Block{{Kind: BlockThinking, Collapsed: true}}, "Thought once"},
		{[]Block{{Kind: BlockError, Collapsed: true}}, "1 error"},
		{[]Block{output()}, "1 output"},
		{[]Block{output(), output()}, "2 outputs"},
	} {
		if got := summarize(c.run); got != c.want {
			t.Fatalf("summarize(%d blocks) = %q, want %q", len(c.run), got, c.want)
		}
	}
}

// The summary is the one line meant to be skipped over, so it is drawn in the
// muted colour rather than the tool colour it used to borrow.
func TestASummaryIsDrawnMuted(t *testing.T) {
	restoreTheme(t)
	SetMode(ModeDark)
	inColor(t)
	line := renderCollapsed([]Block{call("Bash")}, 40)
	if !strings.Contains(line, "38;2;"+rgb(T.Muted)) {
		t.Fatalf("the summary should be muted: %q", line)
	}
	if strings.Contains(line, "38;2;"+rgb(T.Yellow)) {
		t.Fatalf("the summary should not still be yellow: %q", line)
	}
}

// Scrolling back stops the pane chasing new output, which is right while you
// are reading. Sending ends the reading: the message and its answer are what
// you want on screen, so the pane goes back to the bottom.
func TestSendingJumpsBackToTheEnd(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	for i := 0; i < 60; i++ {
		s.tl.Append(Block{Kind: BlockAssistant, Text: "an earlier line"})
	}
	s.tl.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if s.tl.Following() {
		t.Fatal("scrolling up should have stopped follow mode")
	}

	send(t, a, "and now this")
	if !s.tl.Following() {
		t.Fatal("sending must return the pane to the bottom")
	}
	if !strings.Contains(stripSGR(s.tl.View()), "and now this") {
		t.Fatalf("the message just sent is not on screen:\n%s", stripSGR(s.tl.View()))
	}
	s.close()
}

// The same for crema's own commands: what /help prints is the answer to what
// you typed, and belongs on screen.
func TestABuiltinsAnswerIsOnScreen(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	for i := 0; i < 60; i++ {
		s.tl.Append(Block{Kind: BlockAssistant, Text: "an earlier line"})
	}
	s.tl.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	send(t, a, "/help")
	if !strings.Contains(stripSGR(s.tl.View()), "/compact") {
		t.Fatalf("/help printed off screen:\n%s", stripSGR(s.tl.View()))
	}
}
