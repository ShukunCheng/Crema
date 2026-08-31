package ui

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// A branch with a pull request open is a branch whose real state lives on
// GitHub, so the status bar says so: a [ PR #123 ] chip beside the diff and
// theme buttons, coloured by the PR's state, and a click opens it in the
// browser. The lookup is gh's — crema runs `gh pr view` in the agent's
// directory and believes the answer — cached per branch and refreshed every
// few minutes, so the API is asked rarely and the bar never blocks on it.

// PRInfo is the pull request an agent's branch has, as much as a chip needs.
type PRInfo struct {
	Number int    `json:"number"`
	State  string `json:"state"` // OPEN, MERGED, CLOSED
	Draft  bool   `json:"isDraft"`
	URL    string `json:"url"`
}

// prCheckEvery is how stale a cached answer may grow. PRs open and merge on
// the scale of minutes, not keystrokes.
const prCheckEvery = 5 * time.Minute

// ghPRLookup is a seam so tests never call GitHub. The real one asks gh,
// which holds the auth; crema never touches a credential.
var ghPRLookup = func(dir string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", "--json", "number,state,isDraft,url")
	cmd.Dir = dir
	return cmd.Output()
}

type prMsg struct {
	sess   int
	branch string
	pr     *PRInfo
}

// maybeCheckPR refreshes the PR chip when the branch changed or the cached
// answer has aged out. "No PR" is an answer too, and is cached the same way.
func (a *App) maybeCheckPR(s *Session) tea.Cmd {
	branch := s.diff.Branch
	if branch == "" || s.prChecking {
		return nil
	}
	if branch == s.prBranch && time.Since(s.prAt) < prCheckEvery {
		return nil
	}
	s.prChecking = true
	id, dir := s.ID, s.Dir
	return func() tea.Msg {
		msg := prMsg{sess: id, branch: branch}
		if out, err := ghPRLookup(dir); err == nil {
			var pr PRInfo
			if json.Unmarshal(out, &pr) == nil && pr.Number > 0 {
				msg.pr = &pr
			}
		}
		return msg
	}
}
