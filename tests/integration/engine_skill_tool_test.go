package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
)

type scriptedProvider struct {
	responses []*schema.Message
	requests  [][]schema.Message
}

func (p *scriptedProvider) Generate(
	_ context.Context,
	messages []schema.Message,
	_ []schema.ToolDefinition,
) (*schema.Message, error) {
	p.requests = append(p.requests, append([]schema.Message(nil), messages...))
	index := len(p.requests) - 1
	if index >= len(p.responses) {
		return nil, fmt.Errorf("unexpected provider call %d", index+1)
	}
	return p.responses[index], nil
}

func TestAgentEngineProgressivelyReadsSkillWithRealReadFile(t *testing.T) {
	workDir := t.TempDir()
	skillDir := filepath.Join(workDir, "skills", "git-workflow")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const skillLocation = "skills/git-workflow/SKILL.md"
	const skillBody = "progressive-body-secret"
	if err := os.WriteFile(filepath.Join(workDir, filepath.FromSlash(skillLocation)),
		[]byte("---\nname: git-workflow\ndescription: Handle Git workflows\n---\n"+skillBody), 0o600); err != nil {
		t.Fatal(err)
	}
	readTool, err := tools.NewReadFileTool(workDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readTool.Close() })
	registry, err := tools.NewRegistry(tools.RegistryParams{Tools: []tools.Tool{readTool}})
	if err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: blocks("选择 git-workflow，先读取技能。")},
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{
			ID: "read-page-1", Name: "read_file",
			Arguments: json.RawMessage(`{"path":"skills/git-workflow/SKILL.md","limit":4}`),
		}}},
		{Role: schema.RoleAssistant, Content: blocks("发现 continuation marker，继续读取。")},
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{
			ID: "read-page-2", Name: "read_file",
			Arguments: json.RawMessage(`{"path":"skills/git-workflow/SKILL.md","offset":5}`),
		}}},
		{Role: schema.RoleAssistant, Content: blocks("技能已完整读取，可以执行。")},
		{Role: schema.RoleAssistant, Content: blocks("done")},
	}}
	agentEngine := engine.NewAgentEngine(
		provider,
		registry,
		ctxpkg.NewPromptComposer(workDir),
		ctxpkg.NewSkillLoader(workDir),
		workDir,
		true,
	)

	if err := agentEngine.Run(context.Background(), "提交代码", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 6 {
		t.Fatalf("provider calls = %d, want 6", len(provider.requests))
	}
	for _, request := range provider.requests {
		if strings.Contains(messageText(t, request[0]), skillBody) {
			t.Fatalf("System Prompt leaked Skill Body: %q", messageText(t, request[0]))
		}
	}
	firstObservation := findMessageByToolCallID(provider.requests[2], "read-page-1")
	if firstObservation == nil || !strings.Contains(messageText(t, *firstObservation), "Use offset=5") ||
		strings.Contains(messageText(t, *firstObservation), skillBody) {
		t.Fatalf("first observation = %#v", firstObservation)
	}
	secondObservation := findMessageByToolCallID(provider.requests[4], "read-page-2")
	if secondObservation == nil || messageText(t, *secondObservation) != skillBody {
		t.Fatalf("second observation = %#v", secondObservation)
	}
}

func blocks(text string) []schema.ContentBlock {
	return []schema.ContentBlock{schema.TextBlock(text)}
}

func messageText(t *testing.T, message schema.Message) string {
	t.Helper()
	text, err := schema.TextContent(message.Content)
	if err != nil {
		t.Fatalf("TextContent() error = %v", err)
	}
	return text
}

func findMessageByToolCallID(messages []schema.Message, toolCallID string) *schema.Message {
	for index := range messages {
		if messages[index].ToolCallID == toolCallID {
			return &messages[index]
		}
	}
	return nil
}
