package agent

import (
	"context"
	"fmt"
	"os/exec"
)

// Claude drives the official Claude Code CLI headlessly. Auth stays inside the
// CLI (subscription login); crema never reads or passes any credential.
type Claude struct {
	bin      string
	extraEnv []string
}

func NewClaude() *Claude { return &Claude{bin: "claude"} }

func (c *Claude) Name() string  { return "claude" }
func (c *Claude) Label() string { return "Claude Code" }

func (c *Claude) Available() error {
	if _, err := exec.LookPath(c.bin); err != nil {
		return fmt.Errorf("claude CLI not found — install Claude Code and run `claude` once to log in")
	}
	return nil
}

func (c *Claude) args(opts RunOptions) []string {
	a := []string{
		"-p", opts.Prompt,
		"--output-format", "stream-json",
		"--verbose", // required by the CLI when combining -p with stream-json
		"--permission-mode", "acceptEdits",
	}
	if opts.SessionID != "" {
		a = append(a, "--resume", opts.SessionID)
	}
	return a
}

func (c *Claude) Run(ctx context.Context, opts RunOptions) (<-chan Event, error) {
	return runCLI(ctx, c.bin, c.args(opts), opts.Dir, c.extraEnv, &ClaudeParser{})
}
