package agent

import (
	"context"
	"fmt"
	"os/exec"
)

// Codex drives the official OpenAI Codex CLI headlessly (`codex exec --json`).
// Resume is by explicit thread id — never `--last`, which picks the newest
// session on the machine and would hijack a codex running in another terminal.
type Codex struct {
	bin      string
	extraEnv []string
}

func NewCodex() *Codex { return &Codex{bin: "codex"} }

func (c *Codex) Name() string  { return "codex" }
func (c *Codex) Label() string { return "Codex" }

func (c *Codex) Available() error {
	if _, err := exec.LookPath(c.bin); err != nil {
		return fmt.Errorf("codex CLI not found — install Codex and run `codex` once to log in")
	}
	return nil
}

// Modes: codex has no read-only planning mode, so crema doesn't offer one —
// listing a mode the backend would silently ignore is worse than omitting it.
func (c *Codex) Modes() []PermissionMode {
	return []PermissionMode{PermissionDefault, PermissionAcceptEdits, PermissionFull}
}

// Models are left to the CLI's own configuration: codex model availability
// depends on the signed-in plan, so a hardcoded list would offer models the
// user cannot run.
func (c *Codex) Models() []string {
	return []string{DefaultModel}
}

// codexModeFlags maps crema's mode onto codex's sandbox/approval flags.
func codexModeFlags(p PermissionMode) []string {
	switch p {
	case PermissionAcceptEdits:
		return []string{"--full-auto"}
	case PermissionFull:
		return []string{"--dangerously-bypass-approvals-and-sandbox"}
	default:
		return nil
	}
}

func (c *Codex) args(opts RunOptions) []string {
	a := []string{"exec"}
	if opts.SessionID != "" {
		a = append(a, "resume", opts.SessionID)
	}
	a = append(a, "--json")
	a = append(a, codexModeFlags(opts.Permission)...)
	if opts.Model != DefaultModel {
		a = append(a, "--model", opts.Model)
	}
	return append(a, opts.Prompt)
}

func (c *Codex) Run(ctx context.Context, opts RunOptions) (<-chan Event, error) {
	return runCLI(ctx, c.bin, c.args(opts), opts.Dir, c.extraEnv, &CodexParser{})
}
