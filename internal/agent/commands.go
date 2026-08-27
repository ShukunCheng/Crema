package agent

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CommandKind separates the two things a user can invoke by name: a prompt
// file (a slash command) and a skill.
type CommandKind int

const (
	CommandPrompt CommandKind = iota
	CommandSkill
)

func (k CommandKind) String() string {
	if k == CommandSkill {
		return "skill"
	}
	return "command"
}

// Command is one name the backend has installed and can be asked to run.
// Name is what the user types after the leading slash — already namespaced
// when it came from a plugin, so it can be sent to the CLI verbatim.
type Command struct {
	Name  string
	Kind  CommandKind
	Scope string // "project", "user", or the plugin's name
	Desc  string
}

// homeDir is a seam so tests can point discovery at a temporary home.
var homeDir = os.UserHomeDir

// Commands lists the slash commands and skills claude has in dir: the
// project's own, the user's, and every installed plugin's.
func (c *Claude) Commands(dir string) []Command {
	var out []Command
	if dir != "" {
		out = append(out, scanPrompts(filepath.Join(dir, ".claude", "commands"), "project")...)
		out = append(out, scanSkills(filepath.Join(dir, ".claude", "skills"), "project")...)
	}
	home, err := homeDir()
	if err != nil {
		return sortCommands(out)
	}
	out = append(out, scanPrompts(filepath.Join(home, ".claude", "commands"), "user")...)
	out = append(out, scanSkills(filepath.Join(home, ".claude", "skills"), "user")...)
	for _, p := range claudePlugins(home) {
		out = append(out, namespaced(scanPrompts(filepath.Join(p.path, "commands"), p.name), p.name)...)
		out = append(out, namespaced(scanSkills(filepath.Join(p.path, "skills"), p.name), p.name)...)
	}
	return sortCommands(out)
}

// Commands lists codex's custom prompts, which it offers as slash commands.
// Codex has no skills, so nothing here reports one.
func (c *Codex) Commands(dir string) []Command {
	var out []Command
	if dir != "" {
		out = append(out, scanPrompts(filepath.Join(dir, ".codex", "prompts"), "project")...)
	}
	if home, err := homeDir(); err == nil {
		out = append(out, scanPrompts(filepath.Join(home, ".codex", "prompts"), "user")...)
	}
	return sortCommands(out)
}

// Commands is a fixed pair so the demo agent exercises the same UI as a real
// backend. Nothing is read from disk — the mock runs no CLI.
func (m *Mock) Commands(string) []Command {
	return []Command{
		{Name: "demo", Kind: CommandPrompt, Scope: "mock", Desc: "a scripted slash command"},
		{Name: "demo-skill", Kind: CommandSkill, Scope: "mock", Desc: "a scripted skill"},
	}
}

// scanPrompts walks a commands directory. Subdirectories namespace the command
// the way the CLIs do: commands/git/sync.md is /git:sync.
func scanPrompts(root, scope string) []Command {
	var out []Command
	// Errors are swallowed on purpose: a missing or unreadable commands
	// directory is the normal case, not something to report.
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = rel[:len(rel)-len(filepath.Ext(rel))]
		out = append(out, Command{
			Name: strings.ReplaceAll(filepath.ToSlash(rel), "/", ":"),
			Kind: CommandPrompt, Scope: scope, Desc: frontmatterDesc(p),
		})
		return nil
	})
	return out
}

// scanSkills lists the <root>/<name>/SKILL.md directories.
func scanSkills(root, scope string) []Command {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Command
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		manifest := filepath.Join(root, e.Name(), "SKILL.md")
		if _, err := os.Stat(manifest); err != nil {
			continue
		}
		out = append(out, Command{
			Name: e.Name(), Kind: CommandSkill, Scope: scope, Desc: frontmatterDesc(manifest),
		})
	}
	return out
}

// namespaced prefixes plugin-supplied names with the plugin, which is how the
// CLI addresses them: /honeycomb:query-patterns.
func namespaced(cmds []Command, plugin string) []Command {
	for i := range cmds {
		cmds[i].Name = plugin + ":" + cmds[i].Name
	}
	return cmds
}

type plugin struct{ name, path string }

// claudePlugins reads the CLI's own registry of installed plugins. It is read
// rather than walking the plugin cache because the cache also holds every
// marketplace plugin the user has *not* installed.
func claudePlugins(home string) []plugin {
	b, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var reg struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(b, &reg); err != nil {
		return nil
	}
	var out []plugin
	for key, installs := range reg.Plugins {
		name, _, _ := strings.Cut(key, "@") // "code-review@official" → "code-review"
		for _, in := range installs {
			if in.InstallPath != "" {
				out = append(out, plugin{name: name, path: in.InstallPath})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// sortCommands orders by name and drops shadowed duplicates. Callers append
// in precedence order — project before user before plugins — which is the
// order the CLIs themselves resolve a name in, so the first one wins.
func sortCommands(cmds []Command) []Command {
	seen := make(map[string]bool, len(cmds))
	out := cmds[:0]
	for _, c := range cmds {
		key := c.Kind.String() + "/" + c.Name
		if c.Name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// frontmatterDesc reads description out of a markdown file's YAML front
// matter, handling both the plain form and the folded/literal block forms
// (description: >) that long skill descriptions use. Anything else yields "",
// which just means the row is listed without a summary.
func frontmatterDesc(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 1<<20) // skill descriptions run long
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "" // no front matter
	}
	var block []string
	folded := false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		if folded {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if line[0] != ' ' && line[0] != '\t' {
				break // dedented: the next key, so the block ended
			}
			block = append(block, strings.TrimSpace(line))
			continue
		}
		rest, ok := strings.CutPrefix(line, "description:")
		if !ok {
			continue // indented lines land here too, which is right: only the
			// top-level description counts
		}
		v := strings.TrimSpace(rest)
		if v == ">" || v == ">-" || v == "|" || v == "|-" {
			folded = true
			continue
		}
		return unquote(v)
	}
	return strings.Join(block, " ")
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
