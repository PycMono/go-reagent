package context

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestRunContextFactoryDiscoversSkillsAndBuildsClonedInitialContext(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("factory project guide"), 0o600); err != nil {
		t.Fatal(err)
	}
	validDir := filepath.Join(workDir, ".agents", "skills", "review")
	if err := os.MkdirAll(validDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(validDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review changes\n---\nprivate review body"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidDir := filepath.Join(workDir, "skills", "broken")
	if err := os.MkdirAll(invalidDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidDir, "SKILL.md"), []byte("missing frontmatter"), 0o600); err != nil {
		t.Fatal(err)
	}

	definitions := []schema.ToolDefinition{
		{Name: "write", Description: "write files"},
		{Name: "read", Description: "read files", ParallelSafe: true},
	}
	factory := NewRunContextFactory(NewPromptComposer(workDir), NewSkillLoader(workDir))
	runContext, err := factory.Create(context.Background(), "review this", definitions)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(runContext.Messages) != 2 || runContext.Messages[0].Role != schema.RoleSystem || runContext.Messages[1].Role != schema.RoleUser {
		t.Fatalf("Messages = %#v", runContext.Messages)
	}
	systemText := runContextMessageText(t, runContext.Messages[0])
	for _, want := range []string{"factory project guide", "review", "Review changes", ".agents/skills/review/SKILL.md", "sha256:"} {
		if !strings.Contains(systemText, want) {
			t.Fatalf("system message missing %q: %q", want, systemText)
		}
	}
	for _, unwanted := range []string{"private review body", "missing frontmatter"} {
		if strings.Contains(systemText, unwanted) {
			t.Fatalf("system message leaked %q: %q", unwanted, systemText)
		}
	}
	if got := runContextMessageText(t, runContext.Messages[1]); got != "review this" {
		t.Fatalf("user message = %q, want review this", got)
	}
	definitions[0].Name = "mutated"
	if len(runContext.Tools) != 2 || runContext.Tools[0].Name != "write" {
		t.Fatalf("Tools = %#v, want cloned input definitions", runContext.Tools)
	}
}

func TestRunContextFactoryRequiresReadWhenSkillsAreAvailable(t *testing.T) {
	workDir := t.TempDir()
	skillDir := filepath.Join(workDir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review changes\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	factory := NewRunContextFactory(NewPromptComposer(workDir), NewSkillLoader(workDir))

	_, err := factory.Create(context.Background(), "review", []schema.ToolDefinition{{Name: "read_file"}})
	if err == nil || err.Error() != "发现可用 Agent Skills，但 Registry 未挂载 read" {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestRunContextFactoryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	factory := NewRunContextFactory(NewPromptComposer(t.TempDir()), NewSkillLoader(t.TempDir()))

	_, err := factory.Create(ctx, "unused", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v, want context.Canceled", err)
	}
}

func runContextMessageText(t *testing.T, message schema.Message) string {
	t.Helper()
	text, err := schema.TextContent(message.Content)
	if err != nil {
		t.Fatalf("TextContent() error = %v", err)
	}
	return text
}
