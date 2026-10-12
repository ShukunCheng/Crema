package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The answer that prompted this: bold labels in a bulleted list, drawn with
// its asterisks and dashes showing.
func TestAnAnswersMarkdownIsDrawnNotShown(t *testing.T) {
	text := "**What you proved in A and B:**\n" +
		"- **The tab replaces the button (AC1):** the button is gone.\n" +
		"- **History (AC2):** Gomocha shows the `No price template` gap.\n" +
		"- **Pricing unchanged (AC7):** you can't check this in the UI."
	raw := RenderAssistant(text, 100)
	got := stripSGR(raw)
	for _, gone := range []string{"**", "- **", "`"} {
		if strings.Contains(got, gone) {
			t.Fatalf("%q should not be on screen:\n%s", gone, got)
		}
	}
	for _, want := range []string{
		"What you proved in A and B:",
		"• The tab replaces the button (AC1): the button is gone.",
		"• History (AC2): Gomocha shows the  No price template  gap.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(raw, sgrBoldOn+"History (AC2):"+sgrBoldOff) {
		t.Fatalf("the label should be bold:\n%q", raw)
	}
}

func TestMarkdownInline(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"plain text", "plain text"},
		{"**bold** and *italic* and _also_", "bold and italic and also"},
		{"call `go test ./...` now", "call  go test ./...  now"},
		{"snake_case_name stays", "snake_case_name stays"},
		{"2 * 3 * 4 is arithmetic", "2 * 3 * 4 is arithmetic"},
		{`a literal \*star\*`, "a literal *star*"},
		{"see [the docs](https://example.com/x)", "see the docs (https://example.com/x)"},
		{"[https://example.com](https://example.com)", "https://example.com"},
		{"an unclosed **bold", "an unclosed **bold"},
		{"**`code` in bold**", " code  in bold"},
	} {
		if got := stripSGR(inlineMarkdown(c.in, T.Fg)); got != c.want {
			t.Errorf("inline(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMarkdownBlocks(t *testing.T) {
	text := "# Heading one\n" +
		"## Second **level**\n" +
		"1. first\n" +
		"2. second\n" +
		"  - nested\n" +
		"> quoted words\n" +
		"---\n" +
		"```go\n" +
		"x := **not bold**\n" +
		"```\n" +
		"after"
	got := stripSGR(RenderAssistant(text, 60))
	for _, want := range []string{
		"Heading one", "Second level", "1. first", "2. second", "  • nested",
		"▎ quoted words", strings.Repeat("─", 60), "x := **not bold**", "after",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	for _, gone := range []string{"# ", "```", "> "} {
		if strings.Contains(got, gone) {
			t.Fatalf("%q is markup and should not show:\n%s", gone, got)
		}
	}
}

// A list item that wraps continues under its own words, not under the bullet,
// and bold that wraps stays bold on the next row.
func TestAWrappedListItemHangsAndKeepsItsBold(t *testing.T) {
	text := "- **" + strings.TrimSpace(strings.Repeat("word ", 12)) + "** tail"
	raw := RenderAssistant(text, 30)
	rows := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(rows) < 2 {
		t.Fatalf("expected it to wrap:\n%s", stripSGR(raw))
	}
	if second := stripSGR(rows[1]); !strings.HasPrefix(second, "  word") {
		t.Fatalf("the second row should start under the words, got %q", second)
	}
	if !strings.Contains(rows[1], sgrBoldOn) {
		t.Fatalf("the bold should carry onto the second row: %q", rows[1])
	}
}

// Code sits on a grey of its own — inline code as a small box, a fenced block
// as a band the width of the pane — the way a web page shows it.
func TestCodeIsOnAGreyBackground(t *testing.T) {
	restoreTheme(t)
	SetMode(ModeDark)
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	grey := sgrColors(T.Lilac, T.CodeBg)
	if grey == "" {
		t.Fatal("with colour on, the code grey must have a code")
	}
	inline := inlineMarkdown("run `go test` first", T.Fg)
	if !strings.Contains(inline, grey+" go test ") {
		t.Fatalf("inline code should be on the grey: %q", inline)
	}
	fenced := renderMarkdown("```\nx := 1\n```", 40)
	row := strings.Split(fenced, "\n")[0]
	if !strings.Contains(stripSGR(row), " x := 1") || lipgloss.Width(row) != 40 {
		t.Fatalf("a fenced line should be a full-width band: %q", row)
	}
	bg := lipgloss.NewStyle().Background(T.CodeBg).Render("x")
	if !strings.Contains(row, bg[:strings.Index(bg, "x")]) {
		t.Fatalf("and that band should be the code grey: %q", row)
	}
}
