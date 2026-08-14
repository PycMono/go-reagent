package test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
)

func TestBuildCompactionPlanPreservesSystemAndCurrentTurn(t *testing.T) {
	messages := compactionMessages()
	plan, err := harness.BuildCompactionPlan(messages)
	if err != nil {
		t.Fatal(err)
	}
	if texts(plan.SummaryMessages) != "old question\nold answer" {
		t.Fatalf("SummaryMessages = %#v", plan.SummaryMessages)
	}
	if texts(plan.PreservedMessages) != "system\ncurrent question\n\nresult" {
		t.Fatalf("PreservedMessages = %#v", plan.PreservedMessages)
	}
	if plan.PreservedMessages[0].Role != ai.RoleSystem ||
		plan.PreservedMessages[1].Role != ai.RoleUser ||
		plan.PreservedMessages[2].ToolCalls[0].ID != "call-1" ||
		plan.PreservedMessages[3].ToolCallID != "call-1" {
		t.Fatalf("preserved current turn = %#v", plan.PreservedMessages)
	}
}

func TestBuildCompactionPlanKeepsToolCallAndResultTogether(t *testing.T) {
	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: blocks("system")},
		{Role: ai.RoleUser, Content: blocks("old question")},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}}},
		{Role: ai.RoleTool, ToolCallID: "call-1", ToolName: "read", Content: blocks("result")},
		{Role: ai.RoleUser, Content: blocks("current question")},
	}
	plan, err := harness.BuildCompactionPlan(messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.SummaryMessages) != 3 ||
		plan.SummaryMessages[1].ToolCalls[0].ID != "call-1" ||
		plan.SummaryMessages[2].ToolCallID != "call-1" {
		t.Fatalf("tool protocol pair was split: %#v", plan.SummaryMessages)
	}
}

func TestBuildCompactionPlanLimitsSerializedSummaryTo32KiB(t *testing.T) {
	messages := []ai.Message{{Role: ai.RoleSystem, Content: blocks("system")}}
	for index := 0; index < 12; index++ {
		messages = append(messages,
			ai.Message{Role: ai.RoleUser, Content: blocks(strings.Repeat(string(rune('a'+index)), 4096))},
			ai.Message{Role: ai.RoleAssistant, Content: blocks("answer")},
		)
	}
	messages = append(messages, ai.Message{Role: ai.RoleUser, Content: blocks("current question")})

	plan, err := harness.BuildCompactionPlan(messages)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(plan.SummaryMessages)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 32*1024 {
		t.Fatalf("serialized summary = %d bytes", len(encoded))
	}
	if len(plan.SummaryMessages) == 0 || len(plan.SummaryMessages) == len(messages)-2 {
		t.Fatalf("bounded summary selection = %d messages", len(plan.SummaryMessages))
	}
}

func TestBuildCompactionPlanRejectsContextWithoutOldHistory(t *testing.T) {
	_, err := harness.BuildCompactionPlan([]ai.Message{
		{Role: ai.RoleSystem, Content: blocks("system")},
		{Role: ai.RoleUser, Content: blocks("current question")},
	})
	if err == nil {
		t.Fatal("BuildCompactionPlan() error = nil")
	}
}

func TestApplySummaryInsertsInternalSystemMessage(t *testing.T) {
	plan, err := harness.BuildCompactionPlan(compactionMessages())
	if err != nil {
		t.Fatal(err)
	}
	result := harness.ApplySummary(plan, "  concise summary  ")
	if len(result) != len(plan.PreservedMessages)+1 ||
		result[0].Role != ai.RoleSystem ||
		result[1].Role != ai.RoleSystem ||
		texts(result[1:2]) != "# Earlier conversation summary\nconcise summary" ||
		result[2].Role != ai.RoleUser {
		t.Fatalf("ApplySummary() = %#v", result)
	}
	if texts(plan.PreservedMessages) != "system\ncurrent question\n\nresult" {
		t.Fatalf("ApplySummary mutated plan: %#v", plan.PreservedMessages)
	}
}

func compactionMessages() []ai.Message {
	return []ai.Message{
		{Role: ai.RoleSystem, Content: blocks("system")},
		{Role: ai.RoleUser, Content: blocks("old question")},
		{Role: ai.RoleAssistant, Content: blocks("old answer")},
		{Role: ai.RoleUser, Content: blocks("current question")},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}}},
		{Role: ai.RoleTool, ToolCallID: "call-1", ToolName: "read", Content: blocks("result")},
	}
}

func texts(messages []ai.Message) string {
	values := make([]string, 0, len(messages))
	for _, message := range messages {
		text, _ := ai.TextContent(message.Content)
		values = append(values, text)
	}
	return strings.Join(values, "\n")
}
