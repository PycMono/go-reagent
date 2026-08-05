package tools

import (
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
)

func newTestRegistry(
	t *testing.T,
	registrations []agent.MiddlewareRegistration,
	tools ...agent.Tool,
) agent.Registry {
	t.Helper()
	registry, err := agent.NewRegistry(agent.RegistryOptions{
		Tools:       tools,
		Middlewares: registrations,
	})
	if err != nil {
		t.Fatalf("agent.NewRegistry() error = %v", err)
	}
	return registry
}

func toolResultText(t *testing.T, result agent.ToolResult) string {
	t.Helper()
	return toolEventText(t, result.Content)
}

func toolEventText(t *testing.T, content []ai.ContentBlock) string {
	t.Helper()
	text, err := ai.TextContent(content)
	if err != nil {
		t.Fatalf("ai.TextContent() error = %v", err)
	}
	return text
}
