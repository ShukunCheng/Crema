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

func (c *Codex) args(opts RunOptions) []string {
	if opts.SessionID != "" {
		return []string{"exec", "resume", opts.SessionID, "--json", "--full-auto", opts.Prompt}
	}
	return []string{"exec", "--json", "--full-auto", opts.Prompt}
}

func (c *Codex) Run(ctx context.Context, opts RunOptions) (<-chan Event, error) {
	return runCLI(ctx, c.bin, c.args(opts), opts.Dir, c.extraEnv, &CodexParser{})
}
