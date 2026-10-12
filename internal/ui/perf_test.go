package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// The frame is drawn ten times a second while anything is busy, because the
// working line carries a spinner. Anything on that path that costs a walk of
// the whole transcript is paid ten times a second, per agent, in front of the
// next keystroke — which is what "crema feels slow" turned out to be.
//
// Measured on this machine, a 4,000-block conversation:
//
//	SetStatus, the spinner's own path   8,162,680ns  ->  162ns
//	one whole frame                    11,750,000ns  ->  ~4,500,000ns
//
// The 8ms was viewport.SetContent: it re-splits every line of the content and
// re-measures each one's width through an ANSI parser, and the working line
// was part of that content. It is drawn under the viewport now, so a spinner
// tick changes a row and nothing else.

func benchApp(b *testing.B, blocks int) *App {
	b.Helper()
	mk := fastMock()
	reg := &agent.Registry{Agents: []agent.Agent{mk}}
	a := NewApp(reg, mk, b.TempDir())
	a.resize(160, 40)
	s := a.cur()
	var bs []Block
	for i := 0; i < blocks; i++ {
		switch i % 4 {
		case 0:
			bs = append(bs, Block{Kind: BlockUser, Text: fmt.Sprintf("message %d", i)})
		case 1:
			bs = append(bs, Block{Kind: BlockAssistant, Text: strings.Repeat("some prose about the change. ", 12)})
		case 2:
			bs = append(bs, Block{Kind: BlockTool, Name: "Bash", Text: strings.Repeat("go test ./...\n", 20), Collapsed: true})
		default:
			bs = append(bs, Block{Kind: BlockToolOutput, Text: strings.Repeat("ok  github.com/x/y  1.2s\n", 30), Collapsed: true})
		}
	}
	s.tl.Restore(bs)
	s.busy = true
	return a
}

// BenchmarkSpinnerTick is the one that matters: what a single frame of the
// spinner costs on the conversation pane. It must not grow with the
// conversation.
func BenchmarkSpinnerTick(b *testing.B) {
	for _, n := range []int{100, 1000, 8000} {
		b.Run(fmt.Sprintf("blocks=%d", n), func(b *testing.B) {
			a := benchApp(b, n)
			tl := a.cur().tl
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tl.SetStatus(fmt.Sprintf("⣾ working… (%ds)", i))
			}
		})
	}
}

// TestASpinnerTickDoesNotWalkTheTranscript is the same property as a test, so
// it is checked by the ordinary suite rather than only when someone remembers
// to run the benchmarks. It asserts the shape — the working line is not part
// of the content the viewport holds — rather than a timing, which would be
// flaky on a loaded machine.
func TestASpinnerTickDoesNotWalkTheTranscript(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.tl.Restore([]Block{
		{Kind: BlockUser, Text: "do the thing"},
		{Kind: BlockAssistant, Text: "done"},
	})
	before := s.tl.Content()
	s.tl.SetStatus("⣾ working… (3s)")
	if s.tl.Content() != before {
		t.Fatal("the working line must not be part of the conversation the viewport holds")
	}
	if !strings.Contains(stripSGR(s.tl.View()), "working…") {
		t.Fatalf("but it must still be drawn:\n%s", stripSGR(s.tl.View()))
	}
	// And the pane is still exactly as tall as it was told to be.
	s.tl.SetSize(80, 20)
	s.tl.SetStatus("⣽ working… (4s)")
	if got := len(strings.Split(s.tl.View(), "\n")); got != 20 {
		t.Fatalf("the pane is %d rows, want 20 — the working line takes its row from the viewport", got)
	}
	s.tl.SetPending([]string{"and then this"})
	if got := len(strings.Split(s.tl.View(), "\n")); got != 20 {
		t.Fatalf("with a message waiting it is %d rows, want 20", got)
	}
	if !strings.Contains(stripSGR(s.tl.View()), "and then this") {
		t.Fatal("and the waiting message is drawn under the working line")
	}
}

// BenchmarkAppendOnALongConversation is what one streamed event costs the
// interface's goroutine once a conversation is long.
func BenchmarkAppendOnALongConversation(b *testing.B) {
	for _, n := range []int{500, 2000, 8000} {
		b.Run(fmt.Sprintf("blocks=%d", n), func(b *testing.B) {
			a := benchApp(b, n)
			tl := a.cur().tl
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tl.Append(Block{Kind: BlockToolOutput, Text: "ok  github.com/x/y  1.2s", Collapsed: true})
			}
		})
	}
}
