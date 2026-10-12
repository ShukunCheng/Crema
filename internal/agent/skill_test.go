package agent

import "testing"

// A skill's instructions are written into the conversation by the CLI, not by
// the agent: after the Skill tool says "Launching skill", the whole SKILL.md
// follows as a user message marked isSynthetic. Crema drew it as the agent's
// own prose. The shapes here are the CLI's own — isSynthetic on the stream,
// isMeta in the transcript on disk.
func TestASkillsInstructionsAreNotDrawn(t *testing.T) {
	p := &ClaudeParser{}
	launch := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"Launching skill: pr-review"}]},"parent_tool_use_id":null}`
	evs := p.ParseLine([]byte(launch))
	if len(evs) != 1 || evs[0].Kind != KindToolOutput {
		t.Fatalf("the Skill tool's own result is still shown: %+v", evs)
	}
	for _, line := range []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Base directory for this skill: C:\skills\pr-review\n\n# PR review\nStep 1..."}]},"parent_tool_use_id":null,"isSynthetic":true}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Base directory for this skill: ..."}]},"parent_tool_use_id":"toolu_sub","isSynthetic":true}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Base directory for this skill: ..."}]},"isMeta":true}`,
	} {
		if evs := p.ParseLine([]byte(line)); len(evs) != 0 {
			t.Fatalf("the skill's text should not be drawn, got %+v", evs)
		}
	}
	// A subagent's real prompt is still its prompt.
	prompt := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Run echo hi"}]},"parent_tool_use_id":"toolu_sub"}`
	if evs := p.ParseLine([]byte(prompt)); len(evs) != 1 {
		t.Fatalf("an ordinary message must still come through: %+v", evs)
	}
}

// The same inside a subagent's transcript, which is where a subagent that
// loads a skill leaves it.
func TestASkillInASubagentTranscriptIsNotDrawn(t *testing.T) {
	line := `{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"Base directory for this skill: ..."}]}}`
	if evs := subEvents([]byte(line)); len(evs) != 0 {
		t.Fatalf("got %+v", evs)
	}
	if evs := subEvents([]byte(`{"type":"user","message":{"role":"user","content":"the prompt it was given"}}`)); len(evs) != 1 {
		t.Fatalf("the prompt must still come through: %+v", evs)
	}
}
