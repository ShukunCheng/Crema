package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/crlog"
	tea "github.com/charmbracelet/bubbletea"
)

// The CLIs' own slash commands — /clear, /model, /compact — belong to their
// interactive interfaces. A headless run has no interface for them to act on,
// so `claude -p "/clear"` is not a command at all: it is a two-character prompt
// the model reads, thinks about, and answers, for the price of a turn. Measured
// on a real account, each of /clear, /compact and /model cost about $0.29 and
// produced nothing.
//
// So crema does them itself. Everything below either maps onto something crema
// already has (the model panel, the diff pane) or is something crema is in a
// position to do (dropping the conversation, summarising it into a new one).
type builtin struct {
	name string
	desc string
	// run does the thing. Returning a command is how it asks for a turn or a
	// quit; most just change state and return nil.
	run func(a *App, s *Session, arg string) tea.Cmd
	// needsIdle refuses the command while a turn is in flight, for the two
	// that would otherwise pull the conversation out from under it.
	needsIdle bool
}

// Filled in init rather than here: /help lists the table, so the table's own
// initializer would refer to itself.
var builtins []builtin

func init() {
	builtins = []builtin{
		{name: "clear", desc: "start over — the agent forgets this conversation", needsIdle: true, run: runClear},
		{name: "compact", desc: "summarise this conversation and carry it into a fresh one", needsIdle: true, run: runCompact},
		{name: "resume", desc: "point this agent at another of the project's conversations — /resume for the list", needsIdle: true, run: runResume},
		{name: "model", desc: "the model for this agent — /model opus, or /model for the list", run: runModel},
		{name: "permissions", desc: "what this agent is allowed to do", run: runPermissions},
		{name: "effort", desc: "how hard this agent thinks — /effort low|medium|high|xhigh|max, or /effort default", run: runEffort},
		{name: "rename", desc: "call this agent something — /rename api, or /rename to undo", run: runRename},
		{name: "login", desc: "sign in to the backend — crema hands over the terminal and never sees the credential", run: runLogin},
		{name: "logout", desc: "sign out of the backend, on this machine", run: runLogout},
		{name: "whoami", desc: "who you are signed in as — read from disk, costs nothing", run: runWhoami},
		{name: "log", desc: "crema's own diagnostic log — where it is, and the last of it", run: runLog},
		{name: "cost", desc: "what this agent has spent, and how full its windows are", run: runCost},
		{name: "diff", desc: "hide the diff, put it beside the conversation, or full screen", run: runDiff},
		{name: "keytest", desc: "why an enter sends or breaks the line — pick a scenario to test it", run: runKeytest},
		{name: "tasks", desc: "the subagents and background commands this session ran — /tasks <id> shows one's output", run: runTasks},
		{name: "help", desc: "the keys crema answers to", run: runHelp},
		{name: "quit", desc: "close crema", run: runQuit},
		{name: "exit", desc: "close crema", run: runQuit},
	}
}

// interactiveOnly are the CLI's own built-ins that manage its interactive
// session or its installation — settings, diagnostics, the model picker's
// neighbours. Headless there is nothing for them to act on, so typing one gets
// an answer here rather than a paid-for non-answer.
//
// Every name was taken from the slash_commands list a real `claude -p` reports
// (2.1.229), not from memory: an earlier version of this list was two thirds
// names the CLI does not have. Anything crema is unsure about is sent rather
// than blocked — being wrong about a command that works is worse than paying
// for one that doesn't.
var interactiveOnly = []string{
	"agents", "color", "config", "context", "debug", "doctor",
	"extra-usage", "fast", "heapdump", "import", "mcp", "reload-skills",
	"usage", "usage-credits",
}

// builtinItems lists crema's own commands for the / drop-up, tagged so they
// read as crema's rather than the backend's.
func builtinItems() []agent.Command {
	out := make([]agent.Command, 0, len(builtins))
	for _, b := range builtins {
		if b.name == "exit" {
			continue // an alias for /quit; one row is enough
		}
		out = append(out, agent.Command{Name: b.name, Scope: "crema", Desc: b.desc})
	}
	return out
}

// allCommands is everything / can offer, in one alphabetical list: what the
// backend reported it has, what crema found on disk, and what crema does
// itself. A backend command wins a clash, which is the same rule runBuiltin
// applies when the name is sent.
//
// The backend's own list is the important half. Walking the filesystem finds
// the commands and skills someone wrote down, but the CLI's built-ins —
// /init, /security-review, /code-review and the rest — are not files anywhere,
// so before crema asked, they simply could not be offered.
func allCommands(s *Session) []agent.Command {
	out := append([]agent.Command(nil), s.Commands()...)
	for _, name := range s.cliCmds {
		if !hasCommand(out, name) {
			out = append(out, agent.Command{Name: name, Scope: s.Backend.Name()})
		}
	}
	for _, b := range builtinItems() {
		if !hasCommand(out, b.Name) {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// knownToBackend reports whether the backend listed this command itself. Only
// meaningful once it has said — before the first turn crema has no list and
// assumes nothing.
func knownToBackend(s *Session, name string) bool {
	for _, c := range s.cliCmds {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}

// splitCommand pulls "/name rest" apart. ok is false for anything that isn't a
// bare slash command, including a path someone typed at the start of a line.
func splitCommand(text string) (name, arg string, ok bool) {
	if !strings.HasPrefix(text, "/") || strings.HasPrefix(text, "//") {
		return "", "", false
	}
	name, arg, _ = strings.Cut(strings.TrimPrefix(text, "/"), " ")
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "", "", false // a path, not a command
	}
	return strings.ToLower(name), strings.TrimSpace(arg), true
}

// runBuiltin handles what crema can handle itself. handled is false for
// anything that should go to the agent as written, which includes every
// command the backend really has — a project that defines its own /clear keeps
// it, because a command someone wrote down beats one crema assumed.
func (a *App) runBuiltin(s *Session, text string) (tea.Cmd, bool) {
	name, arg, ok := splitCommand(text)
	if !ok || hasCommand(s.Commands(), name) {
		return nil, false
	}
	for _, b := range builtins {
		if b.name != name {
			continue
		}
		if b.needsIdle && s.busy {
			a.note = "/" + name + " waits until this turn finishes — esc cancels it"
			return nil, true
		}
		cmd := b.run(a, s, arg)
		s.tl.ShowAnswer() // whatever it had to say is the answer to what you typed
		return cmd, true
	}
	for _, n := range interactiveOnly {
		if n == name {
			s.tl.Append(Block{Kind: BlockSystem, Text: "/" + name +
				" belongs to the CLI's own interface. Crema drives the CLI headlessly, where " +
				"that command doesn't exist — sending it would just be a prompt the model reads, " +
				"so crema didn't. Type /help for what crema does have."})
			s.tl.ShowAnswer()
			return nil, true
		}
	}
	// A name the backend has never listed is a typo, not a command. Saying so
	// costs nothing; sending it costs a turn.
	if len(s.cliCmds) > 0 && !knownToBackend(s, name) {
		s.tl.Append(Block{Kind: BlockSystem, Text: "/" + name + " is not a command " +
			s.Backend.Label() + " has. Type / to see what it does."})
		s.tl.ShowAnswer()
		return nil, true
	}
	return nil, false
}

func hasCommand(cmds []agent.Command, name string) bool {
	for _, c := range cmds {
		if strings.EqualFold(c.Name, name) {
			return true
		}
	}
	return false
}

func runClear(a *App, s *Session, _ string) tea.Cmd {
	a.warmSoon = true // whatever this closes, a message is coming
	s.reset()
	// Starting over starts the meter over: the dollars belonged to the
	// conversation just dropped. /compact keeps its total, because the work
	// continues there; only the transcript is folded.
	s.cost = 0
	s.tasks = nil
	a.note = "cleared — the next message starts a new session"
	a.persist()
	return nil
}

// compactPrompt is what crema asks for on /compact. The reply is the only
// thing the agent keeps of the conversation, so it has to carry the whole
// working state.
const compactPrompt = `Summarise this conversation so it can be carried into a fresh session.

Cover: what we are working on, the decisions taken and why, the files touched
and what changed in them, what is still unfinished, and anything else you would
need in order to pick this up cold. Name files, functions and commands
explicitly. Reply with the summary and nothing else.`

func runCompact(a *App, s *Session, _ string) tea.Cmd {
	if s.tl.Len() <= 1 {
		a.note = "nothing to compact yet"
		return nil
	}
	s.noteCompacting()
	a.note = "compacting — asking the agent to summarise this conversation"
	return tea.Batch(s.startTurn("/compact", compactPrompt), a.sp.Tick)
}

// finishCompact runs when the summarising turn ends: the agent's conversation
// goes, and the summary it produced is held back to open the next one.
//
// The transcript on screen stays. It is crema's, not the agent's, and what is
// on it is what you were reading: an autocompact fires the moment a turn
// ends, so the answer to your last message is the newest thing there. Wiping
// the pane and showing the summary in its place — pages of it — took that
// answer away before it had been read. So the reply to the summarising turn
// is swapped for one folded line saying what happened, the summary a click
// away behind it, and the conversation carries on underneath.
//
// Anything queued during the summarising turn is kept too: it was typed into
// this conversation, and the folded one is the same conversation, cheaper.
func (a *App) finishCompact(s *Session) {
	s.compacting = false
	summary := s.tl.LastText()
	if strings.TrimSpace(summary) == "" {
		a.note = "compact failed — the conversation is untouched"
		return
	}
	s.closeConv("the conversation was folded up") // the next opens one with no --resume
	s.agentSID = ""
	s.ctxTokens, s.ctxWindow = 0, 0
	s.preamble = "This continues an earlier session. Here is where it had got to:\n\n" + summary
	s.tl.ReplaceAnswer(Block{Kind: BlockSummary, Text: summary, Collapsed: true})
	a.note = "compacted"
	a.warmSoon = true // the folded conversation needs a process of its own
	a.persist()
}

func runModel(a *App, s *Session, arg string) tea.Cmd {
	a.warmSoon = true // whatever this closes, a message is coming
	if arg == "" {
		return a.openControls()
	}
	for _, m := range s.Backend.Models() {
		if m != agent.DefaultModel && strings.EqualFold(m, arg) {
			s.SetModel(m)
			a.persist()
			return nil
		}
	}
	if strings.EqualFold(arg, "default") {
		s.SetModel(agent.DefaultModel)
		a.persist()
		return nil
	}
	a.note = arg + " is not one of " + strings.Join(modelNames(s), ", ")
	return nil
}

func modelNames(s *Session) []string {
	var out []string
	for _, m := range s.Backend.Models() {
		if m == agent.DefaultModel {
			m = "default"
		}
		out = append(out, m)
	}
	return out
}

// runRename names the focused agent, or gives it back its derived name when
// asked for nothing. The sidebar is a list of what you are working on, and
// "claude · api" three times over is not that.
func runRename(a *App, s *Session, arg string) tea.Cmd {
	was := s.Title()
	s.Rename(arg)
	if s.Name == "" {
		a.note = "back to " + s.Title()
	} else {
		a.note = was + " is now " + s.Title()
	}
	a.persist()
	return nil
}

// runEffort is /effort: how hard the agent is asked to think. The CLI has one
// of these too, but interactively — headless it is a command-line flag, so it
// is crema's to carry.
func runEffort(a *App, s *Session, arg string) tea.Cmd {
	a.warmSoon = true // whatever this closes, a message is coming
	levels := s.efforts()
	if len(levels) <= 1 {
		a.note = s.Backend.Label() + " has no effort setting"
		return nil
	}
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		return a.openControls()
	}
	if arg == "default" {
		s.SetEffort(agent.DefaultEffort)
		a.persist()
		return nil
	}
	for _, e := range levels {
		if e != agent.DefaultEffort && e == arg {
			s.SetEffort(e)
			a.persist()
			return nil
		}
	}
	a.note = arg + " is not one of " + strings.Join(effortNames(levels), ", ")
	return nil
}

func effortNames(levels []string) []string {
	out := make([]string, 0, len(levels))
	for _, e := range levels {
		out = append(out, effortLabel(e))
	}
	return out
}

// runLog shows crema's own log: what it decided and when. It is not the
// conversation — that is already on disk under ~/.claude/projects — it is the
// record of crema's own doing, which is what is missing when a turn stops or a
// subagent dies and there is nothing to point at.
//
// It costs nothing to keep and nothing to read: a local file, never sent to a
// model. The only way a log costs tokens is if you paste it into a message.
func runLog(a *App, s *Session, arg string) tea.Cmd {
	p := crlog.Path()
	if p == "" {
		a.note = "the log is not open — crema could not write to its config directory"
		return nil
	}
	n := 40
	if v := parseCount(arg); v > 0 {
		n = v
	}
	lines := crlog.Tail(n)
	head := "crema's log · " + p
	if d := crlog.Dropped(); d > 0 {
		head += fmt.Sprintf(" · %d lines dropped while busy", d)
	}
	if len(lines) == 0 {
		s.tl.Append(Block{Kind: BlockSystem, Text: head + "\n(nothing in it yet)"})
		s.tl.ShowAnswer()
		return nil
	}
	s.tl.Append(Block{Kind: BlockSystem, Text: head})
	s.tl.Append(Block{Kind: BlockToolOutput, Text: strings.Join(lines, "\n"), Collapsed: false})
	s.tl.ShowAnswer()
	return nil
}

// parseCount reads "/log 100", and 0 for anything that is not a count.
func parseCount(arg string) int {
	n := 0
	for _, r := range strings.TrimSpace(arg) {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return min(n, 500)
}

func runPermissions(a *App, _ *Session, _ string) tea.Cmd {
	return a.openControls()
}

func runCost(a *App, s *Session, _ string) tea.Cmd {
	lines := []string{fmt.Sprintf("spent on this agent: $%.4f", s.cost)}
	if f := contextFrac(s.ctxTokens, s.ctxWindow); f >= 0 {
		lines = append(lines, fmt.Sprintf("context: %d%% of %d tokens", int(f*100+0.5), s.ctxWindow))
	}
	for _, lim := range s.limits {
		l := lim.Label() + " window: "
		switch {
		case lim.Known:
			l += fmt.Sprintf("%d%% used", int(lim.Utilization*100+0.5))
		case lim.Surpassed > 0:
			l += fmt.Sprintf("over %d%% used", int(lim.Surpassed*100+0.5))
		default:
			l += "share not reported to a headless run — see the README on the status-line bridge"
		}
		if !lim.ResetsAt.IsZero() {
			l += ", " + resetIn(time.Until(lim.ResetsAt))
		}
		lines = append(lines, l)
	}
	if len(lines) == 1 {
		lines = append(lines, "the backend hasn't reported context or usage yet")
	}
	s.tl.Append(Block{Kind: BlockSystem, Text: strings.Join(lines, "\n")})
	return nil
}

func runDiff(a *App, _ *Session, _ string) tea.Cmd { return a.cycleDiff() }

func runQuit(*App, *Session, string) tea.Cmd { return tea.Quit }

func runHelp(_ *App, s *Session, _ string) tea.Cmd {
	var b strings.Builder
	b.WriteString("crema's own commands:\n")
	for _, c := range builtinItems() {
		fmt.Fprintf(&b, "  /%-12s %s\n", c.Name, c.Desc)
	}
	b.WriteString("\nkeys:\n" +
		"  enter          send — while the agent works, the message waits in line\n" +
		"  ctrl+enter     newline\n" +
		"  /  @           the agent's commands and skills · the project's files\n" +
		"  ↓  ctrl+p      model and permissions\n" +
		"  ctrl+t         diff: hidden → beside → full screen\n" +
		"  ctrl+n ctrl+w  open an agent · close this one\n" +
		"  tab  alt+1..9  next agent · jump to one\n" +
		"  ctrl+c ctrl+q  copy the selection · quit\n" +
		"  esc            cancel this turn, and drop anything queued behind it")
	s.tl.Append(Block{Kind: BlockSystem, Text: b.String()})
	return nil
}
