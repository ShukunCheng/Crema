package ui

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// The agents write markdown, and the CLI draws it: **bold** comes out bold, a
// list gets bullets, a heading stands out. Crema drew tables and nothing else,
// so an answer arrived studded with asterisks and hashes — readable, but only
// just, and the emphasis the model chose was lost.
//
// This is the part of markdown answers actually use, drawn in the pane's own
// terms: inline emphasis and code, headings, lists, quotes, rules and fenced
// code. It is not a full CommonMark parser and does not need to be — anything
// it does not recognise is left exactly as written, which is what the pane
// showed before.
//
// Emphasis is switched on and off with its own codes (bold on, bold off)
// rather than a style's full reset, because a reset would also take away the
// theme's background for the rest of the row.

const (
	sgrBoldOn    = "\x1b[1m"
	sgrBoldOff   = "\x1b[22m"
	sgrItalicOn  = "\x1b[3m"
	sgrItalicOff = "\x1b[23m"
)

// sgrColors is the code that sets the foreground to f and the background to
// b, in whatever colour depth the terminal has — empty when it has none, as
// in tests.
func sgrColors(f, b lipgloss.TerminalColor) string {
	s := lipgloss.NewStyle().Foreground(f).Background(b).Render("x")
	if i := strings.Index(s, "x"); i > 0 {
		return s[:i]
	}
	return ""
}

// codeSGR switches inline code on — the code colour on the code grey — and
// back off to restore on the pane's own background.
func codeSGR(restore lipgloss.Color) (on, off string) {
	return sgrColors(T.Lilac, T.CodeBg), sgrColors(restore, T.Bg)
}

var (
	mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	mdList    = regexp.MustCompile(`^(\s*)([-*+]|\d{1,3}[.)])\s+(.*)$`)
	mdQuote   = regexp.MustCompile(`^\s*>\s?(.*)$`)
	mdRule    = regexp.MustCompile(`^\s*(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)
	mdFence   = regexp.MustCompile("^\\s*(```|~~~)")
)

// renderMarkdown draws a stretch of prose — no tables; those are drawn apart —
// at width w.
func renderMarkdown(text string, w int) string {
	w = max(1, w)
	var out []string
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		if fence != "" {
			if strings.HasPrefix(strings.TrimSpace(line), fence) {
				fence = ""
				continue
			}
			// A fenced block is a grey band the width of the pane, like a
			// code block on a web page, with a column of room on the left.
			out = append(out, base().Background(T.CodeBg).Foreground(T.Lilac).
				Width(w).Render(" "+line))
			continue
		}
		if m := mdFence.FindStringSubmatch(line); m != nil {
			fence = m[1]
			continue // the fence itself is punctuation; the code is what shows
		}
		switch {
		case strings.TrimSpace(line) == "":
			out = append(out, body(w).Render(""))
		case mdRule.MatchString(line):
			out = append(out, fg(T.Muted).Width(w).Render(strings.Repeat("─", w)))
		case mdHeading.MatchString(line):
			m := mdHeading.FindStringSubmatch(line)
			out = append(out, fg(T.Lilac).Bold(true).Width(w).
				Render(inlineMarkdown(m[2], T.Lilac)))
		case mdList.MatchString(line):
			m := mdList.FindStringSubmatch(line)
			indent := min(len(expandTabs(m[1]))/2*2, 8)
			marker := m[2]
			if strings.ContainsAny(marker, "-*+") {
				marker = "•"
			}
			lead := strings.Repeat(" ", indent) + marker + " "
			out = append(out, hanging(lead, inlineMarkdown(m[3], T.Fg), w, T.Fg))
		case mdQuote.MatchString(line):
			m := mdQuote.FindStringSubmatch(line)
			inner := body(max(1, w-2)).Foreground(T.Muted).Italic(true).
				Render(inlineMarkdown(m[1], T.Muted))
			var rows []string
			for _, r := range strings.Split(inner, "\n") {
				rows = append(rows, fg(T.Muted).Render("▎ ")+r)
			}
			out = append(out, strings.Join(rows, "\n"))
		default:
			out = append(out, body(w).Foreground(T.Fg).Render(inlineMarkdown(line, T.Fg)))
		}
	}
	return carryEmphasis(strings.Join(out, "\n"), T.Fg) + "\n"
}

// hanging draws text after lead, and wraps it under itself rather than under
// the lead — a list item's second line starts where its words did.
func hanging(lead, text string, w int, c lipgloss.Color) string {
	lw := lipgloss.Width(lead)
	if w-lw < 8 {
		return body(w).Foreground(c).Render(lead + text)
	}
	rows := strings.Split(body(w-lw).Foreground(c).Render(text), "\n")
	pad := base().Render(strings.Repeat(" ", lw))
	for i := range rows {
		if i == 0 {
			rows[i] = fg(T.Pink).Render(lead) + rows[i]
		} else {
			rows[i] = pad + rows[i]
		}
	}
	return strings.Join(rows, "\n")
}

// inlineMarkdown turns the emphasis inside one line into the terminal's:
// **bold**, *italic*, `code`, [text](url), and a backslash in front of a
// marker to keep it literal. restore is the colour to go back to after code.
// URLs outside code are underlined as they always were.
func inlineMarkdown(s string, restore lipgloss.Color) string {
	var b, plain strings.Builder
	flush := func() {
		b.WriteString(linkify(plain.String()))
		plain.Reset()
	}
	codeOn, codeOff := codeSGR(restore)
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case rest[0] == '\\' && len(rest) > 1 && strings.ContainsRune("\\`*_[]()#+-.!>~|", rune(rest[1])):
			plain.WriteByte(rest[1])
			i += 2
			continue
		case rest[0] == '`':
			n := 1
			for n < len(rest) && rest[n] == '`' {
				n++
			}
			ticks := rest[:n]
			if end := strings.Index(rest[n:], ticks); end >= 0 {
				flush()
				// A space either side inside the grey, so the code sits in
				// its box rather than touching the edges.
				code := strings.TrimSpace(rest[n : n+end])
				b.WriteString(codeOn + " " + code + " " + codeOff)
				i += n + end + n
				continue
			}
		case strings.HasPrefix(rest, "**") || strings.HasPrefix(rest, "__"):
			mark := rest[:2]
			if end := closing(rest[2:], mark); end > 0 && opens(s, i, mark) {
				flush()
				b.WriteString(sgrBoldOn + inlineMarkdown(rest[2:2+end], restore) + sgrBoldOff)
				i += 2 + end + 2
				continue
			}
		case rest[0] == '*' || rest[0] == '_':
			mark := rest[:1]
			if end := closing(rest[1:], mark); end > 0 && opens(s, i, mark) {
				flush()
				b.WriteString(sgrItalicOn + inlineMarkdown(rest[1:1+end], restore) + sgrItalicOff)
				i += 1 + end + 1
				continue
			}
		case rest[0] == '[':
			if text, url, n, ok := mdLink(rest); ok {
				flush()
				label := inlineMarkdown(text, restore)
				if text == url || text == "" {
					b.WriteString(linkify(url))
				} else {
					b.WriteString(label + " (" + linkify(url) + ")")
				}
				i += n
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(rest)
		plain.WriteRune(r)
		i += size
	}
	flush()
	return b.String()
}

// opens reports whether the marker at s[i:] can start emphasis: it must touch
// a word after it, and an underscore must not sit inside one — snake_case
// stays snake_case.
func opens(s string, i int, mark string) bool {
	after, _ := utf8.DecodeRuneInString(s[i+len(mark):])
	if unicode.IsSpace(after) || after == utf8.RuneError {
		return false
	}
	if mark[0] == '_' && i > 0 {
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		if unicode.IsLetter(before) || unicode.IsDigit(before) {
			return false
		}
	}
	return true
}

// closing is where the emphasis started by mark ends in s, or -1: the next
// mark that follows a non-space, and for an underscore is not inside a word.
func closing(s, mark string) int {
	for from := 0; from < len(s); {
		j := strings.Index(s[from:], mark)
		if j < 0 {
			return -1
		}
		j += from
		before, _ := utf8.DecodeLastRuneInString(s[:j])
		ok := j > 0 && !unicode.IsSpace(before)
		if ok && mark[0] == '_' {
			after, _ := utf8.DecodeRuneInString(s[j+len(mark):])
			ok = !(unicode.IsLetter(after) || unicode.IsDigit(after))
		}
		// A single * is not the end of a ** — skip a run of the marker that
		// is longer than the one being closed.
		if ok && len(mark) == 1 && strings.HasPrefix(s[j+1:], mark) {
			from = j + 2
			continue
		}
		if ok {
			return j
		}
		from = j + len(mark)
	}
	return -1
}

// mdLink reads [text](url) at the start of s.
func mdLink(s string) (text, url string, n int, ok bool) {
	close := strings.Index(s, "](")
	if close < 1 || strings.Contains(s[:close], "\n") {
		return "", "", 0, false
	}
	end := strings.IndexByte(s[close+2:], ')')
	if end < 0 {
		return "", "", 0, false
	}
	url = strings.TrimSpace(s[close+2 : close+2+end])
	if url == "" || strings.ContainsAny(url, " \t") {
		return "", "", 0, false
	}
	return s[1:close], url, close + 2 + end + 1, true
}

// carryEmphasis re-opens bold, italics and code at the start of each row that
// a wrap split them across. Every row is its own styled line, and the style
// in front of it would otherwise start the row plain.
func carryEmphasis(s string, restore lipgloss.Color) string {
	codeOn, codeOff := codeSGR(restore)
	type toggle struct{ on, off string }
	toggles := []toggle{{sgrBoldOn, sgrBoldOff}, {sgrItalicOn, sgrItalicOff}}
	if codeOn != "" && codeOn != codeOff {
		toggles = append(toggles, toggle{codeOn, codeOff})
	}
	active := make([]bool, len(toggles))
	rows := strings.Split(s, "\n")
	for n, row := range rows {
		var carry strings.Builder
		for k, t := range toggles {
			if active[k] {
				carry.WriteString(t.on)
			}
		}
		if carry.Len() > 0 {
			at := leadingSGR(row)
			rows[n] = row[:at] + carry.String() + row[at:]
		}
		for k, t := range toggles {
			on, off := strings.LastIndex(row, t.on), strings.LastIndex(row, t.off)
			switch {
			case on > off:
				active[k] = true
			case off > on:
				active[k] = false
			}
		}
	}
	return strings.Join(rows, "\n")
}

// leadingSGR is the length of the run of escape codes a row starts with — the
// row's own style, which anything carried over has to come after.
func leadingSGR(row string) int {
	i := 0
	for strings.HasPrefix(row[i:], "\x1b[") {
		j := strings.IndexByte(row[i:], 'm')
		if j < 0 {
			break
		}
		i += j + 1
	}
	return i
}

// expandTabs counts a tab as four spaces, for measuring list indents.
func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// stripInline drops emphasis markers and keeps the words, for places that are
// styled as a whole — a table's header row is bold already.
func stripInline(s string) string {
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	return s
}
