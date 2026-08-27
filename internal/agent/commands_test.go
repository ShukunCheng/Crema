package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeHome points discovery at a temporary home for one test.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	old := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = old })
	return home
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findCmd(t *testing.T, cmds []Command, name string) Command {
	t.Helper()
	for _, c := range cmds {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no command %q in %v", name, names(cmds))
	return Command{}
}

func names(cmds []Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}

func TestClaudeCommandsCollectsProjectUserAndPlugin(t *testing.T) {
	home := fakeHome(t)
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, ".claude", "commands", "ship.md"),
		"---\ndescription: ship it\n---\nbody\n")
	writeFile(t, filepath.Join(dir, ".claude", "skills", "reviewing", "SKILL.md"),
		"---\nname: reviewing\ndescription: review the diff\n---\n")
	writeFile(t, filepath.Join(home, ".claude", "commands", "notes.md"), "no front matter\n")

	pluginDir := filepath.Join(home, "plugincache", "honeycomb")
	writeFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
		`{"plugins":{"honeycomb@hc-marketplace":[{"installPath":`+quoteJSON(pluginDir)+`}]}}`)
	writeFile(t, filepath.Join(pluginDir, "commands", "setup.md"),
		"---\ndescription: connect the MCP server\n---\n")
	writeFile(t, filepath.Join(pluginDir, "skills", "query-patterns", "SKILL.md"),
		"---\ndescription: how to query\n---\n")

	got := (&Claude{}).Commands(dir)

	ship := findCmd(t, got, "ship")
	if ship.Kind != CommandPrompt || ship.Scope != "project" || ship.Desc != "ship it" {
		t.Fatalf("ship = %+v", ship)
	}
	if s := findCmd(t, got, "reviewing"); s.Kind != CommandSkill || s.Scope != "project" {
		t.Fatalf("reviewing = %+v", s)
	}
	if n := findCmd(t, got, "notes"); n.Scope != "user" || n.Desc != "" {
		t.Fatalf("notes = %+v", n)
	}
	if p := findCmd(t, got, "honeycomb:setup"); p.Scope != "honeycomb" || p.Desc != "connect the MCP server" {
		t.Fatalf("honeycomb:setup = %+v", p)
	}
	if p := findCmd(t, got, "honeycomb:query-patterns"); p.Kind != CommandSkill {
		t.Fatalf("honeycomb:query-patterns = %+v", p)
	}
}

// quoteJSON escapes a Windows path for embedding in a JSON literal.
func quoteJSON(s string) string {
	out := make([]rune, 0, len(s)+8)
	out = append(out, '"')
	for _, r := range s {
		if r == '\\' || r == '"' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(append(out, '"'))
}

func TestClaudeCommandsNamespacesSubdirectories(t *testing.T) {
	fakeHome(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".claude", "commands", "git", "sync.md"), "body\n")

	if c := findCmd(t, (&Claude{}).Commands(dir), "git:sync"); c.Scope != "project" {
		t.Fatalf("git:sync = %+v", c)
	}
}

func TestClaudeCommandsProjectShadowsUser(t *testing.T) {
	home := fakeHome(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".claude", "commands", "ship.md"),
		"---\ndescription: the project one\n---\n")
	writeFile(t, filepath.Join(home, ".claude", "commands", "ship.md"),
		"---\ndescription: the user one\n---\n")

	got := (&Claude{}).Commands(dir)
	seen := 0
	for _, c := range got {
		if c.Name == "ship" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("ship listed %d times, want 1: %v", seen, names(got))
	}
	if c := findCmd(t, got, "ship"); c.Desc != "the project one" {
		t.Fatalf("user command shadowed the project one: %+v", c)
	}
}

func TestClaudeCommandsSortedAndEmptyWhenNothingInstalled(t *testing.T) {
	fakeHome(t)
	dir := t.TempDir()
	for _, n := range []string{"zulu", "alpha", "mike"} {
		writeFile(t, filepath.Join(dir, ".claude", "commands", n+".md"), "body\n")
	}
	got := names((&Claude{}).Commands(dir))
	want := []string{"alpha", "mike", "zulu"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %v, want %v", got, want)
		}
	}
	if c := (&Claude{}).Commands(t.TempDir()); len(c) != 0 {
		t.Fatalf("bare directory yielded %v", names(c))
	}
}

func TestCodexCommandsReadsPrompts(t *testing.T) {
	home := fakeHome(t)
	writeFile(t, filepath.Join(home, ".codex", "prompts", "refactor.md"),
		"---\ndescription: tidy up\n---\n")

	got := (&Codex{}).Commands(t.TempDir())
	if c := findCmd(t, got, "refactor"); c.Kind != CommandPrompt || c.Scope != "user" || c.Desc != "tidy up" {
		t.Fatalf("refactor = %+v", c)
	}
}

func TestFrontmatterDesc(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, body, want string
	}{
		{"plain", "---\ndescription: a summary\n---\nbody", "a summary"},
		{"quoted", "---\ndescription: \"a summary\"\n---\n", "a summary"},
		{"folded", "---\nname: x\ndescription: >\n  first line\n  second line\nallowed-tools: Read\n---\n",
			"first line second line"},
		{"literal", "---\ndescription: |\n  only line\n---\n", "only line"},
		{"missing", "---\nname: x\n---\nbody", ""},
		{"none", "# just a heading\n", ""},
		{"nested key ignored", "---\nmetadata:\n  description: not this\n---\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(dir, c.name+".md")
			writeFile(t, p, c.body)
			if got := frontmatterDesc(p); got != c.want {
				t.Fatalf("frontmatterDesc = %q, want %q", got, c.want)
			}
		})
	}
}
