package ui

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// authMock is a backend that can be signed in and out without touching the
// real login. Nothing here ever runs `claude auth logout`, which would sign
// this machine out for every agent and every other Claude Code on it.
type authMock struct {
	*agent.Mock
	in       bool
	outCalls int
	failOut  error
}

func (m *authMock) LoginCmd() *exec.Cmd { return exec.Command("cmd", "/c", "exit", "0") }

func (m *authMock) Logout() error {
	m.outCalls++
	if m.failOut != nil {
		return m.failOut
	}
	m.in = false
	return nil
}

func (m *authMock) AuthStatus() (agent.AuthInfo, error) {
	if !m.in {
		return agent.AuthInfo{}, nil
	}
	return agent.AuthInfo{LoggedIn: true, Method: "claude.ai",
		Email: "someone@example.com", Plan: "max", Organization: "Some Org"}, nil
}

func authApp(t *testing.T) (*App, *Session, *authMock) {
	t.Helper()
	a := testApp(t)
	m := &authMock{Mock: agent.NewMock(), in: true}
	s := a.cur()
	s.Backend = m
	return a, s, m
}

// /whoami reads the CLI's own status off disk, so it says who you are without
// calling the API and without costing a token.
func TestWhoamiSaysWhoYouAre(t *testing.T) {
	a, s, m := authApp(t)
	send(t, a, "/whoami")
	got := stripSGR(s.tl.Content())
	for _, want := range []string{"signed in", "someone@example.com", "max", "Some Org"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing:\n%s", want, lastLines(got, 4))
		}
	}
	m.in = false
	send(t, a, "/whoami")
	if got := stripSGR(s.tl.Content()); !strings.Contains(got, "not signed in") ||
		!strings.Contains(got, "/login signs in") {
		t.Fatalf("signed out should say so and name the way back:\n%s", lastLines(got, 4))
	}
}

// Signing out is not local to one agent: one login serves every agent on the
// machine, so every held process of that backend is let go rather than left
// running on a credential that no longer works.
func TestLogoutDropsEveryProcessOfThatBackend(t *testing.T) {
	a, s, m := authApp(t)
	other := a.addSession(m, t.TempDir())
	elsewhere := a.addSession(agent.NewMock(), t.TempDir()) // a different backend

	a.selectSession(0) // adding agents moved the focus; /logout acts on the focused one
	send(t, a, "/logout")
	if m.outCalls != 1 {
		t.Fatalf("logout ran %d times, want 1", m.outCalls)
	}
	for _, x := range []*Session{s, other} {
		if x.conv != nil {
			t.Fatal("a process on the old credential should be let go")
		}
	}
	_ = elsewhere // a different backend's login is not this one's business
	if got := stripSGR(s.tl.Content()); !strings.Contains(got, "not signed in") {
		t.Fatalf("and it should say where that left you:\n%s", lastLines(got, 4))
	}
}

// A logout that fails says so rather than pretending.
func TestAFailedLogoutSaysSo(t *testing.T) {
	a, s, m := authApp(t)
	m.failOut = errors.New("the CLI said no")
	send(t, a, "/logout")
	got := stripSGR(s.tl.Content())
	if !strings.Contains(got, "could not sign out") || !strings.Contains(got, "the CLI said no") {
		t.Fatalf("it should report the failure:\n%s", lastLines(got, 4))
	}
	if !m.in {
		t.Fatal("and must not claim you are signed out when you are not")
	}
}

// The terminal cannot be handed away underneath a running turn: the login
// would be writing to a screen that is not there.
func TestLoginWaitsForTheTurnToFinish(t *testing.T) {
	a, s, _ := authApp(t)
	s.busy = true
	send(t, a, "/login")
	if !strings.Contains(a.note, "wait for this turn") {
		t.Fatalf("note = %q", a.note)
	}
	s.busy = false
	send(t, a, "/login")
	if got := stripSGR(s.tl.Content()); !strings.Contains(got, "handing the terminal to") {
		t.Fatalf("and when idle it should hand the terminal over:\n%s", lastLines(got, 4))
	}
}

// What comes back after the terminal is crema's again.
func TestLoginReportsWhatItCameTo(t *testing.T) {
	a, s, _ := authApp(t)
	a.Update(loginDoneMsg{sess: s.ID})
	if got := stripSGR(s.tl.Content()); !strings.Contains(got, "someone@example.com") {
		t.Fatalf("it should say who you are now:\n%s", lastLines(got, 4))
	}
	a.Update(loginDoneMsg{sess: s.ID, err: errors.New("browser never answered")})
	if got := stripSGR(s.tl.Content()); !strings.Contains(got, "signing in did not finish") {
		t.Fatalf("and a failure should be reported:\n%s", lastLines(got, 4))
	}
}

// A backend with no login of its own is not offered one.
func TestABackendWithoutALoginSaysSo(t *testing.T) {
	a := testApp(t) // the plain mock has no Authenticator
	send(t, a, "/login")
	if !strings.Contains(a.note, "no login crema can reach") {
		t.Fatalf("note = %q", a.note)
	}
}
