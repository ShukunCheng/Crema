package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

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

	app := ui.NewAppRestored(reg, saved, cur, abs)
	if typed["dir"] || typed["agent"] {
		app.EnsureSession(cur, abs) // an explicit target always gets focus
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

func fail(err error) {
	fmt.Fprintln(os.Stderr, "crema:", err)
	os.Exit(1)
}
