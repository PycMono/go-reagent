package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/config"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/tools"
	"go.uber.org/fx/fxtest"
)

type scriptedProvider struct {
	responses []*ai.Message
	requests  [][]ai.Message
}

func (p *scriptedProvider) Generate(
	_ context.Context,
	messages []ai.Message,
	_ []ai.ToolDefinition,
) (*ai.Message, error) {
	p.requests = append(p.requests, append([]ai.Message(nil), messages...))
	index := len(p.requests) - 1
	if index >= len(p.responses) {
		return nil, fmt.Errorf("unexpected provider call %d", index+1)
	}
	return p.responses[index], nil
}

func TestAgentRuntimeProgressivelyReadsSkillWithRealReadTool(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("You are a Git workflow Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
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
	lifecycle := fxtest.NewLifecycle(t)
	workspace, err := tools.NewWorkspace(lifecycle, config.WorkDir(workDir))
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.RequireStart()
	t.Cleanup(lifecycle.RequireStop)
	readTool, err := tools.NewReadTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := agent.NewRegistry(agent.RegistryOptions{Tools: []agent.Tool{readTool}})
	if err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("选择 git-workflow，先读取技能。")},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{
			ID: "read-page-1", Name: "read",
			Arguments: json.RawMessage(`{"path":"skills/git-workflow/SKILL.md","limit":4}`),
		}}},
		{Role: ai.RoleAssistant, Content: blocks("发现 continuation marker，继续读取。")},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{
			ID: "read-page-2", Name: "read",
			Arguments: json.RawMessage(`{"path":"skills/git-workflow/SKILL.md","offset":5}`),
		}}},
		{Role: ai.RoleAssistant, Content: blocks("技能已完整读取，可以执行。")},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	factory := ctxpkg.NewRunContextFactory(
		ctxpkg.NewPromptComposer(workDir),
		ctxpkg.NewSkillLoader(workDir),
	)
	loop := engine.NewAgentLoop(provider, engine.NewToolScheduler(registry, 4), true)
	runtime := engine.NewAgentRuntime(factory, loop, registry)

	_, err = runtime.Run(context.Background(), agent.RunRequest{Input: ai.Message{
		Role:    ai.RoleUser,
		Content: blocks("提交代码"),
	}}, nil)
	if err != nil {
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
	firstObservation := findToolObservation(provider.requests[2], "read-page-1")
	if firstObservation == nil || !strings.Contains(messageText(t, *firstObservation), "Use offset=5") ||
		strings.Contains(messageText(t, *firstObservation), skillBody) {
		t.Fatalf("first observation = %#v", firstObservation)
	}
	secondObservation := findToolObservation(provider.requests[4], "read-page-2")
	if secondObservation == nil || messageText(t, *secondObservation) != skillBody {
		t.Fatalf("second observation = %#v", secondObservation)
	}
}

func blocks(text string) []ai.ContentBlock {
	return []ai.ContentBlock{ai.TextBlock(text)}
}

func messageText(t *testing.T, message ai.Message) string {
	t.Helper()
	text, err := ai.TextContent(message.Content)
	if err != nil {
		t.Fatalf("TextContent() error = %v", err)
	}
	return text
}

func findToolObservation(messages []ai.Message, toolCallID string) *ai.Message {
	for index := range messages {
		if messages[index].Role == ai.RoleTool && messages[index].ToolCallID == toolCallID {
			return &messages[index]
		}
	}
	return nil
}
