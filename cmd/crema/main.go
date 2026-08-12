package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	agentName := flag.String("agent", "", "agent to start with: claude | codex | mock (default: first available)")
	dir := flag.String("dir", ".", "working directory the agent runs in")
	doctor := flag.Bool("doctor", false, "check the environment and exit")
	theme := flag.String("theme", "auto", "color theme: auto | light | dark (toggle at runtime with ctrl+l)")
	fresh := flag.Bool("fresh", false, "start with one new agent instead of restoring the last session")
	perm := flag.String("permission-mode", "acceptEdits",
		"what the agent may do without asking: default | plan | acceptEdits | full (change later with ctrl+p)")
	model := flag.String("model", "", "model for the first agent, e.g. opus | sonnet | haiku (default: the CLI's own)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	// Which flags the user actually typed, so restoring doesn't quietly
	// override an explicit --theme or ignore an explicit --dir.
	typed := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { typed[f.Name] = true })

	if *showVersion {
		fmt.Println("crema", Version)
		return
	}

	abs, err := filepath.Abs(*dir)
	if err != nil {
		fail(err)
	}
	reg := agent.NewRegistry()

	if *doctor {
		report, ok := doctorReport(reg, abs)
		fmt.Print(report)
		if !ok {
			os.Exit(1)
		}
		return
	}

	cur, err := pick(reg, *agentName)
	if err != nil {
		fail(err)
	}
	var saved ui.State
	if !*fresh {
		saved = ui.LoadState()
	}

	switch {
	case typed["theme"] || saved.Theme == "":
		switch *theme {
		case "light":
			ui.SetMode(ui.ModeLight)
		case "dark":
			ui.SetMode(ui.ModeDark)
		case "auto", "":
			ui.SetMode(ui.DetectMode()) // asks the terminal about its background
		default:
			fail(fmt.Errorf("unknown theme %q (want auto, light, or dark)", *theme))
		}
	case saved.Theme == "light":
		ui.SetMode(ui.ModeLight)
	default:
		ui.SetMode(ui.ModeDark)
	}

	ui.SyncTerminalBackground()
	defer ui.ResetTerminalBackground() // don't leave the shell recolored

	mode, err := parseMode(*perm, cur)
	if err != nil {
		fail(err)
	}
	app := ui.NewAppRestored(reg, saved, cur, abs)
	if typed["dir"] || typed["agent"] {
		app.EnsureSession(cur, abs) // an explicit target always gets focus
	}
	if typed["permission-mode"] || typed["model"] {
		app.ApplyToFocused(typed["permission-mode"], mode, typed["model"], *model)
	}
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		ui.ResetTerminalBackground()
		fail(err)
	}
}

func pick(reg *agent.Registry, name string) (agent.Agent, error) {
	if name == "" {
		if a := reg.FirstAvailable(); a != nil {
			return a, nil
		}
		return nil, errors.New("no agent available — run `crema --doctor` to see what's missing")
	}
	var known []string
	for _, a := range reg.Agents {
		known = append(known, a.Name())
		if a.Name() == name {
			if err := a.Available(); err != nil {
				return nil, err
			}
			return a, nil
		}
	}
	return nil, fmt.Errorf("unknown agent %q (available: %v)", name, known)
}

// parseMode validates --permission-mode against what the chosen backend can
// actually honor, so an unsupported mode fails loudly instead of being ignored.
func parseMode(s string, backend agent.Agent) (agent.PermissionMode, error) {
	m := agent.PermissionMode(s)
	for _, ok := range backend.Modes() {
		if ok == m {
			return m, nil
		}
	}
	var names []string
	for _, ok := range backend.Modes() {
		names = append(names, string(ok))
	}
	return "", fmt.Errorf("%s does not support permission mode %q (it supports: %s)",
		backend.Label(), s, strings.Join(names, ", "))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "crema:", err)
	os.Exit(1)
}
