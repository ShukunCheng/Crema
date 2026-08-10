package main

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/gitdiff"
)

// doctorReport describes the environment. ok is true only when a real agent
// (not the demo mock) is installed and logged in.
func doctorReport(reg *agent.Registry, dir string) (string, bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "crema %s\n\nagents:\n", Version)
	ok := false
	for _, ag := range reg.Agents {
		if err := ag.Available(); err != nil {
			fmt.Fprintf(&b, "  ✗ %s — %v\n", ag.Label(), err)
			continue
		}
		fmt.Fprintf(&b, "  ✓ %s\n", ag.Label())
		if ag.Name() != "mock" {
			ok = true
		}
	}
	b.WriteString("\nworkspace:\n")
	ds := gitdiff.Collect(dir)
	if ds.Err != "" {
		fmt.Fprintf(&b, "  ✗ %s — crema still runs, the diff pane just stays empty\n", ds.Err)
	} else {
		fmt.Fprintf(&b, "  ✓ git repo at %s — %d changed files, +%d −%d\n",
			ds.Repo, len(ds.Files), ds.Additions, ds.Deletions)
	}
	if !ok {
		b.WriteString("\nno coding agent found. install one and sign in with your subscription:\n" +
			"  Claude Code  https://claude.com/claude-code   then run: claude\n" +
			"  Codex        npm i -g @openai/codex           then run: codex\n" +
			"crema never asks for an API key — the CLI owns your login.\n")
	}
	return b.String(), ok
}
