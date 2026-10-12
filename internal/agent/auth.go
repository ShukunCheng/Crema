package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// contextWithTimeout keeps the two short commands from hanging the interface
// if the CLI ever stops answering.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// Signing in is the one thing crema deliberately cannot do for you.
//
// Crema never sees a credential: the whole point of driving the official CLIs
// is that your subscription login stays theirs. So /login does not ask you for
// anything — it hands the terminal to the CLI's own login, which opens a
// browser and talks to Anthropic directly, and takes the terminal back when
// that is done. Crema's part is to get out of the way and to say what the
// answer was.
//
// Measured against the CLI's own surface (2.1.259): `claude auth login`,
// `claude auth logout`, and `claude auth status`, which prints JSON and costs
// nothing because it reads a file rather than calling the API.

// Authenticator is a backend whose login crema can reach. Optional: a backend
// without one is simply not offered /login.
type Authenticator interface {
	// LoginCmd is the command to run with the terminal handed over. It is
	// interactive — it opens a browser and waits — so it cannot be run in a
	// pipe behind the interface.
	LoginCmd() *exec.Cmd
	// Logout signs out. Non-interactive, so crema can just run it.
	Logout() error
	// AuthStatus is who you are signed in as, read from disk rather than
	// asked of the API, so it costs nothing to call.
	AuthStatus() (AuthInfo, error)
}

// AuthInfo is as much of the CLI's status as is worth saying out loud. The
// fields crema does not show — org ids, directories — are left out rather than
// carried around.
type AuthInfo struct {
	LoggedIn     bool   `json:"loggedIn"`
	Method       string `json:"authMethod"`
	Email        string `json:"email"`
	Organization string `json:"orgName"`
	Plan         string `json:"subscriptionType"`
}

// Describe is the one-line account of who you are, for the conversation.
func (a AuthInfo) Describe() string {
	if !a.LoggedIn {
		return "not signed in"
	}
	parts := []string{"signed in"}
	if a.Email != "" {
		parts = append(parts, "as "+a.Email)
	}
	if a.Plan != "" {
		parts = append(parts, "on "+a.Plan)
	}
	if a.Method != "" {
		parts = append(parts, "via "+a.Method)
	}
	return strings.Join(parts, " · ")
}

func (c *Claude) LoginCmd() *exec.Cmd {
	cmd := exec.Command(c.bin, "auth", "login")
	cmd.Env = append(os.Environ(), c.extraEnv...)
	return cmd
}

func (c *Claude) Logout() error {
	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin, "auth", "logout")
	cmd.Env = append(os.Environ(), c.extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Claude) AuthStatus() (AuthInfo, error) {
	ctx, cancel := contextWithTimeout(20 * time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin, "auth", "status")
	cmd.Env = append(os.Environ(), c.extraEnv...)
	out, err := cmd.Output()
	if err != nil {
		return AuthInfo{}, err
	}
	// The CLI prints JSON, but it is not crema's JSON: an older or newer build
	// may say more or less, and a field crema does not know about is not a
	// reason to report a failure.
	var info AuthInfo
	if err := decodeAuth(out, &info); err != nil {
		return AuthInfo{}, err
	}
	return info, nil
}

// decodeAuth reads the CLI's status. It is not crema's JSON — an older or
// newer build may say more or less, and it may print a notice before it — so
// a field crema does not know about is ignored and a line in front is skipped.
func decodeAuth(b []byte, into *AuthInfo) error {
	if err := json.Unmarshal(trimToJSON(b), into); err != nil {
		return fmt.Errorf("could not read the CLI's auth status: %w", err)
	}
	return nil
}

// trimToJSON drops anything the CLI printed before the object — a warning, a
// version notice — so one stray line does not make the status unreadable.
func trimToJSON(b []byte) []byte {
	if i := strings.IndexByte(string(b), '{'); i > 0 {
		return b[i:]
	}
	return b
}
