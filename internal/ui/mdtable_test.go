package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

const triggerTable = `Why each trigger exists:

| Trigger | Fires when | Without it |
|---|---|---|
| trg_fv_materialmutation_ReservedQuantity | A reservation line is added, edited, deleted, or parts are booked from stock | The column never moves at all |
| trg_fv_activity_ReservedQuantity | An order is completed, cancelled, or re-opened | Closed orders keep their stock reserved forever |

And a closing line.`

// A markdown table is drawn as a table: columns sized to the pane, long
// cells wrapped inside their own column, never mid-row across the pane.
func TestAMarkdownTableKeepsItsColumns(t *testing.T) {
	out := stripSGR(RenderAssistant(triggerTable, 100))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	bars := 0
	for _, ln := range lines {
		if lipgloss.Width(ln) > 100 {
			t.Fatalf("wider than the pane: %q", ln)
		}
		if strings.Count(ln, "│") == 4 {
			bars++
		}
	}
	if bars < 5 { // header, its rule, and the body rows with their wraps
		t.Fatalf("want aligned rows with four bars each, got %d in:\n%s", bars, out)
	}
	if !strings.Contains(out, "Why each trigger exists:") || !strings.Contains(out, "And a closing line.") {
		t.Fatal("the prose around the table must survive")
	}
	if strings.Contains(out, "|---") {
		t.Fatal("the markdown rule is replaced by the drawn one")
	}
	// The long cell wrapped inside its column: its words appear, but never on
	// one overlong line.
	if !strings.Contains(out, "booked from") {
		t.Fatalf("cell content lost:\n%s", out)
	}
}

// Fenced code keeps its pipes: a | in a shell command is not a table.
func TestFencedCodeIsNeverATable(t *testing.T) {
	text := "Run this:\n```\nps aux | grep crema\n|--|--|\n```\ndone"
	out := stripSGR(RenderAssistant(text, 80))
	if !strings.Contains(out, "ps aux | grep crema") {
		t.Fatalf("code mangled:\n%s", out)
	}
	if strings.Contains(out, "│") {
		t.Fatalf("nothing here is a table:\n%s", out)
	}
}

// Ordinary prose with a stray pipe stays prose; only |-row + rule is a table.
func TestAStrayPipeIsNotATable(t *testing.T) {
	out := stripSGR(RenderAssistant("a | b\nplain text", 80))
	if strings.Contains(out, "│") {
		t.Fatalf("no rule row, no table:\n%s", out)
	}
}

// Cells shed their markdown dressing and keep an escaped pipe.
func TestCellsAreCleanedAndEscapedPipesKept(t *testing.T) {
	cells := tableCells(`| **legacy** | a \| b | ` + "`code`" + ` |`)
	if len(cells) != 3 || cells[0] != "legacy" || cells[1] != "a | b" || cells[2] != "code" {
		t.Fatalf("cells = %q", cells)
	}
}

// A pane too narrow for the columns falls back to readable text rather than
// four-character confetti.
func TestATinyPaneFallsBackToText(t *testing.T) {
	out := stripSGR(RenderAssistant(triggerTable, 18))
	if strings.Contains(out, "│") {
		t.Fatalf("18 columns cannot hold three columns:\n%s", out)
	}
	if !strings.Contains(out, "Trigger · Fires") {
		t.Fatalf("the fallback should still read as rows:\n%s", out)
	}
}
