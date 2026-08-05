package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/skills"
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

	definitions := []ai.ToolDefinition{
		{Name: "write", Description: "write files"},
		{Name: "read", Description: "read files", ParallelSafe: true},
	}
	factory := NewRunContextFactory(NewPromptComposer(workDir), skills.NewLoader(workDir))
	runContext, err := factory.Create(context.Background(), runRequest("review this"), definitions)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(runContext.Messages) != 2 || runContext.Messages[0].Role != ai.RoleSystem || runContext.Messages[1].Role != ai.RoleUser {
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

func TestRunContextFactoryAssemblesStructuredRequest(t *testing.T) {
	workDir := t.TempDir()
	writeValidAgentWorkspace(t, workDir)
	factory := NewRunContextFactory(NewPromptComposer(workDir), skills.NewLoader(workDir))
	history := []ai.Message{{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("previous answer")}}}
	contextBlocks := []agent.ContextBlock{
		{Name: "preferences", Content: "prefers concise replies", Priority: 10},
		{Name: "customer", Content: "customer tier is gold", Priority: 100},
	}
	metadata := map[string]string{"conversationId": "conversation-1"}
	request := agent.RunRequest{
		RunID:   "run-1",
		History: history,
		Input: ai.Message{
			Role:    ai.RoleUser,
			Content: []ai.ContentBlock{ai.TextBlock("where is my order?")},
		},
		Context:  contextBlocks,
		Metadata: metadata,
	}

	runContext, err := factory.Create(context.Background(), request, []ai.ToolDefinition{{Name: "read"}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(runContext.Messages) != 5 {
		t.Fatalf("Messages count = %d, want 5: %#v", len(runContext.Messages), runContext.Messages)
	}
	wantRoles := []ai.Role{
		ai.RoleSystem,
		ai.RoleSystem,
		ai.RoleSystem,
		ai.RoleAssistant,
		ai.RoleUser,
	}
	for index, want := range wantRoles {
		if got := runContext.Messages[index].Role; got != want {
			t.Fatalf("Messages[%d].Role = %q, want %q", index, got, want)
		}
	}
	wantText := []string{
		"# Context: customer\ncustomer tier is gold",
		"# Context: preferences\nprefers concise replies",
		"previous answer",
		"where is my order?",
	}
	for offset, want := range wantText {
		if got := runContextMessageText(t, runContext.Messages[offset+1]); got != want {
			t.Fatalf("Messages[%d] text = %q, want %q", offset+1, got, want)
		}
	}

	history[0] = ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("mutated history")}}
	contextBlocks[0] = agent.ContextBlock{Name: "mutated", Content: "mutated", Priority: 1000}
	metadata["conversationId"] = "mutated"
	if got := runContextMessageText(t, runContext.Messages[3]); got != "previous answer" {
		t.Fatalf("history was not cloned: %q", got)
	}
	if got := runContextMessageText(t, runContext.Messages[2]); got != "# Context: preferences\nprefers concise replies" {
		t.Fatalf("context was not cloned: %q", got)
	}
	if got := runContext.Metadata["conversationId"]; got != "conversation-1" {
		t.Fatalf("Metadata[conversationId] = %q, want conversation-1", got)
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
	factory := NewRunContextFactory(NewPromptComposer(workDir), skills.NewLoader(workDir))

	_, err := factory.Create(context.Background(), runRequest("review"), []ai.ToolDefinition{{Name: "read_file"}})
	if err == nil || err.Error() != "agent runtime: required tool read is not registered" {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestRunContextFactoryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	factory := NewRunContextFactory(NewPromptComposer(t.TempDir()), skills.NewLoader(t.TempDir()))

	_, err := factory.Create(ctx, runRequest("unused"), []ai.ToolDefinition{{Name: "read"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v, want context.Canceled", err)
	}
}

func runRequest(input string) agent.RunRequest {
	return agent.RunRequest{Input: ai.Message{
		Role:    ai.RoleUser,
		Content: []ai.ContentBlock{ai.TextBlock(input)},
	}}
}

func writeValidAgentWorkspace(t *testing.T, workDir string) {
	t.Helper()
	writeAgentsInstructions(t, workDir, "You are a test Agent.")
	skillDir := filepath.Join(workDir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: test-skill\ndescription: Test workspace behavior\n---\nFollow the test workflow."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runContextMessageText(t *testing.T, message ai.Message) string {
	t.Helper()
	text, err := ai.TextContent(message.Content)
	if err != nil {
		t.Fatalf("TextContent() error = %v", err)
	}
	return text
}
