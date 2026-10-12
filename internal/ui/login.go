package ui

import (
	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/crlog"
	tea "github.com/charmbracelet/bubbletea"
)

// Signing in is the one thing crema deliberately cannot do for you, and
// /login is how it gets out of the way properly.
//
// The CLI's login is interactive: it opens a browser and waits. Crema holds
// the terminal — alt screen, mouse capture — so there is nowhere for that to
// happen until crema lets go. tea.ExecProcess is exactly that: it puts the
// terminal back the way the CLI expects it, runs the command, and takes it
// back afterwards. Crema never sees the credential, which is the whole point
// of driving the official CLI in the first place.

// loginDoneMsg carries what happened back into the interface once the terminal
// is crema's again.
type loginDoneMsg struct {
	sess int
	err  error
	out  bool // true for /logout, so the note reads right
}

// authOf is the backend's login, or nothing when it has none.
func authOf(s *Session) (agent.Authenticator, bool) {
	a, ok := s.Backend.(agent.Authenticator)
	return a, ok
}

// runLogin hands the terminal to the CLI's own login.
func runLogin(a *App, s *Session, _ string) tea.Cmd {
	auth, ok := authOf(s)
	if !ok {
		a.note = s.Backend.Label() + " has no login crema can reach"
		return nil
	}
	// Whatever is running is running against the old credential. A turn that
	// is mid-flight while the terminal is handed away would be writing to a
	// screen that is not there.
	if s.busy {
		a.note = "wait for this turn to finish, or esc to cancel it, then /login"
		return nil
	}
	crlog.Log("auth.login.start", "agent", s.ID, "backend", s.Backend.Name())
	s.tl.Append(Block{Kind: BlockSystem, Text: "handing the terminal to " +
		s.Backend.Label() + " to sign in — crema never sees the credential. " +
		"Finish in the browser; crema comes back when it is done."})
	s.tl.ShowAnswer()

	cmd := auth.LoginCmd()
	id := s.ID
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return loginDoneMsg{sess: id, err: err}
	})
}

// runLogout signs out. It is not interactive, so it needs no terminal — but it
// is not local to this agent either: one login serves every agent and every
// other Claude Code on the machine, so everything holding a process is told.
func runLogout(a *App, s *Session, _ string) tea.Cmd {
	auth, ok := authOf(s)
	if !ok {
		a.note = s.Backend.Label() + " has no login crema can reach"
		return nil
	}
	crlog.Log("auth.logout", "agent", s.ID, "backend", s.Backend.Name())
	if err := auth.Logout(); err != nil {
		s.tl.Append(Block{Kind: BlockError, Text: "could not sign out: " + err.Error()})
		return nil
	}
	a.dropCredentialledProcesses(s.Backend.Name(), "you signed out")
	// Signing out is synchronous — there is no terminal to hand over and
	// nothing to wait for — so it reports itself rather than going round
	// through a message for the sake of symmetry.
	a.finishLogin(loginDoneMsg{sess: s.ID, out: true})
	return nil
}

// dropCredentialledProcesses closes every held process of the same backend.
// They are all running on the credential that just changed, and a process that
// cannot authenticate is worth less than the cold start of replacing it.
func (a *App) dropCredentialledProcesses(backend, why string) {
	for _, x := range a.sessions {
		if x.Backend.Name() == backend && x.conv != nil {
			x.closeConv(why)
		}
	}
}

// finishLogin reports what the login or logout came to. The status is read
// from disk rather than asked of the API, so saying it costs nothing.
func (a *App) finishLogin(msg loginDoneMsg) {
	s := a.sessionByID(msg.sess)
	if s == nil {
		return
	}
	verb := "signing in"
	if msg.out {
		verb = "signing out"
	}
	if msg.err != nil {
		crlog.Log("auth.done", "agent", s.ID, "out", msg.out, "err", msg.err.Error())
		s.tl.Append(Block{Kind: BlockError, Text: verb + " did not finish: " + msg.err.Error()})
		s.tl.ShowAnswer()
		return
	}
	if !msg.out {
		// A fresh credential is no use to a process that started with the old
		// one, and a cold start is cheaper than a turn that fails.
		a.dropCredentialledProcesses(s.Backend.Name(), "you signed in again")
	}
	line := verb + " finished"
	if auth, ok := authOf(s); ok {
		if info, err := auth.AuthStatus(); err == nil {
			line = info.Describe()
			crlog.Log("auth.done", "agent", s.ID, "out", msg.out,
				"logged_in", info.LoggedIn, "method", info.Method, "plan", info.Plan)
		} else {
			crlog.Log("auth.done", "agent", s.ID, "out", msg.out, "status_err", err.Error())
		}
	}
	s.tl.Append(Block{Kind: BlockSystem, Text: line})
	s.tl.ShowAnswer()
	a.note = line
}

// runWhoami says who you are signed in as, without signing anything in or out.
// It reads the CLI's own status file, so it costs no tokens.
func runWhoami(a *App, s *Session, _ string) tea.Cmd {
	auth, ok := authOf(s)
	if !ok {
		a.note = s.Backend.Label() + " has no login crema can reach"
		return nil
	}
	info, err := auth.AuthStatus()
	if err != nil {
		s.tl.Append(Block{Kind: BlockError, Text: "could not read the login status: " + err.Error()})
		s.tl.ShowAnswer()
		return nil
	}
	line := info.Describe()
	if info.LoggedIn && info.Organization != "" {
		line += "\n" + info.Organization
	}
	if !info.LoggedIn {
		line += " — /login signs in"
	}
	s.tl.Append(Block{Kind: BlockSystem, Text: line})
	s.tl.ShowAnswer()
	return nil
}
