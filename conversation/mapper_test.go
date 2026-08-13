package conversation

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

func TestMessageDomainMappingPreservesHistoricalContent(t *testing.T) {
	want := ai.Message{
		Role:       ai.RoleAssistant,
		Content:    []ai.ContentBlock{{Type: ai.ContentTypeText, Text: "answer"}},
		ToolCalls:  []ai.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"AGENTS.md"}`)}},
		ToolCallID: "call-0",
		ToolName:   "read",
		IsError:    true,
	}

	domain := messagesToDomain([]ai.Message{want}, "run-1")
	if domain[0].RunID != "run-1" || domain[0].Payload.Content[0].Text != "answer" ||
		string(domain[0].Payload.ToolCalls[0].Arguments) != `{"path":"AGENTS.md"}` {
		t.Fatalf("mapped domain message = %#v", domain[0])
	}
}

func TestMessagesToHistoryKeepsOnlyCustomerAndFinalAIText(t *testing.T) {
	createdAt := time.Date(2026, 8, 13, 17, 23, 54, 0, time.Local)
	messages := []*conversationentity.Message{
		{ID: "customer-1", Role: conversationentity.RoleUser, CreatedAt: createdAt, Payload: historyTextPayload("问题")},
		{ID: "assistant-tool-call", Role: conversationentity.RoleAssistant, Payload: conversationentity.MessagePayload{
			ToolCalls: []conversationentity.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)}},
		}},
		{ID: "tool-result", Role: conversationentity.RoleTool, Payload: historyTextPayload("内部工具结果")},
		{ID: "assistant-1", Role: conversationentity.RoleAssistant, CreatedAt: createdAt, Payload: historyTextPayload("回答")},
	}

	got, err := messagesToHistory(messages)
	if err != nil {
		t.Fatal(err)
	}
	want := []pi.HistoryMessage{{
		ContentType: pi.HistoryContentTypeText,
		CreateTime:  "2026-08-13 17:23:54",
		CreateTS:    "1786613034000",
		Content:     "问题",
		ID:          "customer-1",
		SenderType:  pi.HistorySenderTypeCustomer,
	}, {
		ContentType: pi.HistoryContentTypeText,
		CreateTime:  "2026-08-13 17:23:54",
		CreateTS:    "1786613034000",
		Content:     "回答",
		ID:          "assistant-1",
		SenderType:  pi.HistorySenderTypeAI,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %#v, want %#v", got, want)
	}
}

func historyTextPayload(content string) conversationentity.MessagePayload {
	return conversationentity.MessagePayload{Content: []conversationentity.ContentBlock{{
		Type: conversationentity.ContentTypeText,
		Text: content,
	}}}
}

func TestInvocationDomainMappingPreservesUsage(t *testing.T) {
	want := []pi.ModelInvocation{{
		Sequence: 2,
		Phase:    pi.ModelInvocationPhaseAction,
		Usage: ai.Usage{
			InputTokens: 120, OutputTokens: 30,
			InputPriceUSDPerMillionTokens: 0.15, OutputPriceUSDPerMillionTokens: 0.60,
			CostUSD: 0.000036, LatencyMS: 245, PlatformID: "zhipu", Model: "glm-4.5-air",
		},
	}}

	got := invocationsToDomain(want, "run-1")
	if len(got) != 1 || got[0].Sequence != 2 || got[0].Phase != "action" ||
		got[0].PlatformID != "zhipu" || got[0].CostUSD != 0.000036 || got[0].RunID != "run-1" {
		t.Fatalf("mapped invocations = %#v", got)
	}
}
