package ui

import (
	"encoding/json"
	"strings"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
)

// An editing tool's arguments arrive as JSON: the file, the text that was
// there, the text that replaces it. Printed raw that is a wall of escaped
// newlines — so crema reads the arguments it knows and draws the edit as what
// it is, the removed lines in red under the added ones in green, the way the
// diff pane and every review tool show a change.

// editArgs covers the editing tools of both CLIs. Fields absent from a given
// tool's arguments simply stay empty.
type editArgs struct {
	FilePath  string     `json:"file_path"`
	OldString string     `json:"old_string"`
	NewString string     `json:"new_string"`
	Content   string     `json:"content"`    // Write: the whole new file
	NewSource string     `json:"new_source"` // NotebookEdit
	Edits     []editArgs `json:"edits"`      // MultiEdit
}

// editLines is the diff of one edit, ready to colour.
type editLines struct {
	kind gitdiff.LineKind
	text string
}

// renderEdit draws a file-editing tool's arguments as a diff, or returns ""
// when they are not an edit this understands — a Read, a Bash, a tool from an
// MCP server — and the caller falls back to printing them as they came.
func renderEdit(tool, input string, w int) string {
	if !changesFiles(tool) {
		return ""
	}
	var args editArgs
	if json.Unmarshal([]byte(input), &args) != nil {
		return ""
	}
	lines := editBody(args)
	if len(lines) == 0 {
		return ""
	}

	var out []string
	if args.FilePath != "" {
		out = append(out, fg(T.Lilac).Width(w).Render(clip(args.FilePath, w)))
	}
	for _, l := range lines {
		switch l.kind {
		case gitdiff.LineAdd:
			out = append(out, addLine().Width(w).Render(clip("+ "+l.text, w)))
		case gitdiff.LineDel:
			out = append(out, delLine().Width(w).Render(clip("- "+l.text, w)))
		default:
			out = append(out, fg(T.Muted).Width(w).Render(clip("  "+l.text, w)))
		}
	}
	return strings.Join(out, "\n")
}

// editBody turns one tool's arguments into diff lines. A Write has no old
// text, so all of it is an addition; a MultiEdit is several edits in a row.
func editBody(args editArgs) []editLines {
	if len(args.Edits) > 0 {
		var all []editLines
		for i, e := range args.Edits {
			if i > 0 {
				all = append(all, editLines{text: ""}) // a blank line between them
			}
			all = append(all, diffStrings(e.OldString, firstNonEmpty(e.NewString, e.Content))...)
		}
		return all
	}
	return diffStrings(args.OldString, firstNonEmpty(args.NewString, args.Content, args.NewSource))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// diffCells caps the comparison below. Beyond it the edit is big enough that
// the cheap answer will do, and nobody is reading it line by line anyway.
const diffCells = 250_000

// diffStrings compares the before and after line by line, so an edit reads as
// the lines it actually replaced — a line both sides share stays context even
// when it sits between two changes, which is the difference between a diff and
// a pair of blocks.
func diffStrings(before, after string) []editLines {
	old, nw := splitEditLines(before), splitEditLines(after)
	switch {
	case len(old) == 0 && len(nw) == 0:
		return nil
	case len(old)*len(nw) > diffCells:
		return trimmedDiff(old, nw)
	}
	return lcsDiff(old, nw)
}

// lcsDiff walks the longest common subsequence of the two sides: shared lines
// are context, and what each side has of its own is a removal or an addition.
func lcsDiff(a, b []string) []editLines {
	n, m := len(a), len(b)
	// common[i][j] is the length of the longest subsequence shared by a[i:]
	// and b[j:], which is what says whether to step down one side or the other.
	common := make([][]int, n+1)
	for i := range common {
		common[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}

	var out []editLines
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, editLines{text: a[i]})
			i, j = i+1, j+1
		case common[i+1][j] >= common[i][j+1]:
			out = append(out, editLines{kind: gitdiff.LineDel, text: a[i]})
			i++
		default:
			out = append(out, editLines{kind: gitdiff.LineAdd, text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, editLines{kind: gitdiff.LineDel, text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, editLines{kind: gitdiff.LineAdd, text: b[j]})
	}
	return out
}

// trimmedDiff is the cheap comparison for an edit too big to line up properly:
// the lines the two sides share at each end are context, and everything
// between them is called changed.
func trimmedDiff(old, nw []string) []editLines {
	head := 0
	for head < len(old) && head < len(nw) && old[head] == nw[head] {
		head++
	}
	tail := 0
	for tail < len(old)-head && tail < len(nw)-head &&
		old[len(old)-1-tail] == nw[len(nw)-1-tail] {
		tail++
	}

	var out []editLines
	for _, l := range old[:head] {
		out = append(out, editLines{text: l})
	}
	for _, l := range old[head : len(old)-tail] {
		out = append(out, editLines{kind: gitdiff.LineDel, text: l})
	}
	for _, l := range nw[head : len(nw)-tail] {
		out = append(out, editLines{kind: gitdiff.LineAdd, text: l})
	}
	for _, l := range old[len(old)-tail:] {
		out = append(out, editLines{text: l})
	}
	return out
}

func splitEditLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}
