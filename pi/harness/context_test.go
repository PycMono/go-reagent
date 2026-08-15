package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

func TestContextBuilderAllowsWorkspaceWithoutSkillsOrRead(t *testing.T) {
	root := writeContextTestWorkspace(t, false)
	builder := NewContextBuilder(NewPromptComposer(root), root)

	got, err := builder.Build(context.Background(), ContextRequest{Input: contextTestUserMessage("你好")}, nil)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(got.Messages))
	}
	if len(got.Tools) != 0 {
		t.Fatalf("tools = %#v, want empty", got.Tools)
	}
	systemText, err := ai.TextContent(got.Messages[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemText, "test chat agent") || strings.Contains(systemText, "<available_skills>") {
		t.Fatalf("system prompt = %q", systemText)
	}
}

func TestContextBuilderRequiresReadOnlyWhenWorkspaceHasSkills(t *testing.T) {
	root := writeContextTestWorkspace(t, true)
	builder := NewContextBuilder(NewPromptComposer(root), root)

	_, err := builder.Build(context.Background(), ContextRequest{Input: contextTestUserMessage("执行流程")}, nil)
	if err == nil || err.Error() != "agent runtime: required tool read is not registered" {
		t.Fatalf("Build() error = %v", err)
	}

	got, err := builder.Build(
		context.Background(),
		ContextRequest{Input: contextTestUserMessage("执行流程")},
		[]ai.ToolDefinition{{Name: "read"}},
	)
	if err != nil {
		t.Fatalf("Build() with read error = %v", err)
	}
	systemText, err := ai.TextContent(got.Messages[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemText, "<available_skills>") || !strings.Contains(systemText, "skills/example/SKILL.md") {
		t.Fatalf("system prompt missing Skill catalog: %q", systemText)
	}
}

func writeContextTestWorkspace(t *testing.T, withSkill bool) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a test chat agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	if !withSkill {
		return root
	}
	skillDir := filepath.Join(root, "skills", "example")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: example\ndescription: Example workflow\n---\n\n# Example\n\nFollow the workflow.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func contextTestUserMessage(content string) ai.Message {
	return ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock(content)}}
}
