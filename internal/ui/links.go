package ui

import (
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A URL in the conversation is a place the agent is pointing at — a pipeline
// run, a pull request, a doc — and the terminal's own ctrl+click never gets
// the chance: crema owns the mouse for dragging and folding, and the classic
// console host has no hyperlinks at all. So crema does both halves itself:
// URLs render underlined so they read as links, and a click on one hands it
// to the OS browser.

// urlPattern is deliberately plain: a scheme the OS will open, then
// everything a URL can hold. What prose hangs on the end is trimmed after.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

// trimURL drops the punctuation a sentence leaves on a link — "see
// https://x.test/y." is not a URL ending in a dot. A closing parenthesis
// stays when the URL itself opened one, for wikipedia-shaped paths.
func trimURL(u string) string {
	for len(u) > 0 {
		c := u[len(u)-1]
		switch c {
		case '.', ',', ';', ':', '!', '?', ']', '}', '>':
			u = u[:len(u)-1]
		case ')':
			if strings.Count(u, "(") >= strings.Count(u, ")") {
				return u
			}
			u = u[:len(u)-1]
		default:
			return u
		}
	}
	return u
}

// linkify underlines every URL in text. Only the underline toggles — no
// colors, no resets — so it nests inside any styled block without knocking
// the block's own colors out from under the rest of the line.
func linkify(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(m string) string {
		u := trimURL(m)
		return "\x1b[4m" + u + "\x1b[24m" + m[len(u):]
	})
}

// findURLs lists the URLs in a block's text, trimmed the way they render.
func findURLs(text string) []string {
	var out []string
	for _, m := range urlPattern.FindAllString(text, -1) {
		if u := trimURL(m); u != "" {
			out = append(out, u)
		}
	}
	return out
}

// LinkAt is the URL drawn at (contentLine, col), or "" when the click is not
// on one. The rendered line may hold only a wrapped fragment of a long URL,
// so what is found on screen is resolved against the block's own text.
func (t *Timeline) LinkAt(contentLine, col int) string {
	line := 0
	for i, r := range t.rendered {
		if r == "" {
			continue // folded into the run above
		}
		rows := strings.Split(strings.TrimSuffix(r, "\n"), "\n")
		if contentLine < line+len(rows) {
			return urlUnder(ansi.Strip(rows[contentLine-line]), col, t.blocks[i].Text)
		}
		line += len(rows)
	}
	return ""
}

func urlUnder(plain string, col int, blockText string) string {
	for _, loc := range urlPattern.FindAllStringIndex(plain, -1) {
		u := trimURL(plain[loc[0]:loc[1]])
		start := lipgloss.Width(plain[:loc[0]])
		if col < start || col >= start+lipgloss.Width(u) {
			continue
		}
		// A wrapped URL shows only its head on this line; the block knows
		// the whole of it.
		for _, full := range findURLs(blockText) {
			if strings.Contains(full, u) {
				return full
			}
		}
		return u
	}
	return ""
}

// openURL is a seam so tests never launch a browser.
var openURL = launchURL

// launchURL hands the link to the OS. rundll32 rather than `cmd /c start`,
// because start is a shell builtin and a & in a query string would be a
// command separator to it.
func launchURL(u string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	case "darwin":
		return exec.Command("open", u).Start()
	}
	return exec.Command("xdg-open", u).Start()
}
