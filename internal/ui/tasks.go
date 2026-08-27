package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// The CLI's model can put work in the background — launch a subagent, or run
// a command without waiting for it — and in the CLI's own interface you can
// look in on that work. Headless, the lifecycle streams past as task events
// and the full output lands in a file the notification names. /tasks is the
// looking-in: the list, and any task's output on request.

const taskTailLines = 40

func runTasks(a *App, s *Session, arg string) tea.Cmd {
	if len(s.tasks) == 0 {
		a.note = "no subagents or background commands yet this session"
		return nil
	}
	if arg != "" {
		for _, tk := range s.tasks {
			if strings.HasPrefix(tk.ID, arg) {
				s.tl.Append(Block{Kind: BlockSystem, Text: taskDetail(tk)})
				return nil
			}
		}
		a.note = arg + " matches no task — /tasks lists them"
		return nil
	}
	var b strings.Builder
	b.WriteString("background work this session — /tasks <id> shows one's output:\n")
	for _, tk := range s.tasks {
		fmt.Fprintf(&b, "  %-10s %-10s %s\n", tk.ID, tk.Status, taskLabel(tk))
	}
	s.tl.Append(Block{Kind: BlockSystem, Text: strings.TrimRight(b.String(), "\n")})
	return nil
}

// taskLabel is one task in a line: what kind of thing it is, what it was
// asked, and how far along it has said it is.
func taskLabel(tk agent.TaskUpdate) string {
	kind := tk.Type
	if kind == "local_bash" {
		kind = "shell"
	}
	l := kind + " · " + tk.Desc
	if tk.Tokens > 0 {
		l += fmt.Sprintf(" · %s tokens", shortCount(tk.Tokens))
	}
	if tk.Status == "running" && tk.LastTool != "" {
		l += " · now: " + tk.LastTool
	}
	return l
}

// taskDetail is everything crema knows about one task, ending with the tail
// of the output file the CLI wrote for it. The file is the CLI's own record —
// crema reads it, never rewrites it.
func taskDetail(tk agent.TaskUpdate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\n%s", tk.ID, tk.Status, taskLabel(tk))
	if tk.Summary != "" {
		b.WriteString("\n\nits closing words:\n" + tk.Summary)
	}
	switch out, err := tailFile(tk.OutputFile, taskTailLines); {
	case tk.OutputFile == "":
		b.WriteString("\n\nno output file reported yet")
	case err != nil:
		b.WriteString("\n\ncould not read its output: " + err.Error())
	case out == "":
		b.WriteString("\n\nits output file is empty so far — " + tk.OutputFile)
	default:
		fmt.Fprintf(&b, "\n\nthe last of its output (%s):\n%s", tk.OutputFile, out)
	}
	return b.String()
}

// tailFile is the last n lines of a file, enough to see how work is going
// without paging a subagent's whole transcript into the conversation.
func tailFile(path string, n int) (string, error) {
	if path == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\r\n"), "\n")
	if len(lines) > n {
		lines = append([]string{fmt.Sprintf("… %d earlier lines (crema shows the last %d)", len(lines)-n, n)}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n"), nil
}
