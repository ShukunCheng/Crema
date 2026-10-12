package agent

import (
	"strings"
	"testing"
)

// The exact bytes `claude auth status` printed on this machine (2.1.259).
// Parsing real output rather than output invented to match the parser is the
// difference between a test and a restatement.
const realAuthStatus = `{
  "loggedIn": true,
  "authMethod": "claude.ai",
  "apiProvider": "firstParty",
  "analyticsDisabled": false,
  "projectsDirectory": "C:\\Users\\Someone\\.claude\\projects",
  "configDirectory": "C:\\Users\\Someone\\.claude",
  "email": "someone@example.com",
  "orgId": "065d2516-2097-4ffd-9e64-26ae678ebe9d",
  "orgName": "Someone's Organization",
  "subscriptionType": "max"
}`

func TestAuthStatusReadsTheCLIsOwnOutput(t *testing.T) {
	var info AuthInfo
	if err := decodeAuth([]byte(realAuthStatus), &info); err != nil {
		t.Fatal(err)
	}
	if !info.LoggedIn || info.Email != "someone@example.com" || info.Plan != "max" {
		t.Fatalf("info = %+v", info)
	}
	got := info.Describe()
	for _, want := range []string{"signed in", "someone@example.com", "max", "claude.ai"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from %q", want, got)
		}
	}
}

// A field crema does not know about is not a reason to report a failure, and
// neither is a line printed in front of the JSON.
func TestAuthStatusToleratesTheUnexpected(t *testing.T) {
	noisy := "a new version is available\n" + realAuthStatus
	var info AuthInfo
	if err := decodeAuth([]byte(noisy), &info); err != nil {
		t.Fatalf("a notice before the JSON should not break it: %v", err)
	}
	if !info.LoggedIn {
		t.Fatal("and the status should still be read")
	}
	var out AuthInfo
	if err := decodeAuth([]byte(`{"loggedIn":false}`), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.Describe(); got != "not signed in" {
		t.Fatalf("signed out should say so plainly: %q", got)
	}
}
