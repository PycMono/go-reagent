package conversation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/governor"
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
	want.ToolCalls[0].Arguments[0] = 'x'
	if string(domain[0].Payload.ToolCalls[0].Arguments) != `{"path":"AGENTS.md"}` {
		t.Fatalf("mapped domain message aliases runtime input: %#v", domain[0])
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
	want := []pi.Message{{
		ContentType: "text",
		CreateTime:  "2026-08-13 17:23:54",
		CreateTS:    "1786613034000",
		Content:     "问题",
		ID:          "customer-1",
		SenderType:  "customer",
	}, {
		ContentType: "text",
		CreateTime:  "2026-08-13 17:23:54",
		CreateTS:    "1786613034000",
		Content:     "回答",
		ID:          "assistant-1",
		SenderType:  "ai",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %#v, want %#v", got, want)
	}
}

func TestMessageDomainMappingPreservesImageURL(t *testing.T) {
	source := ai.Message{
		Role: ai.RoleUser,
		Content: []ai.ContentBlock{
			ai.TextBlock("看图"),
			ai.ImageBlock("https://example.com/a.png"),
		},
	}

	domain := messagesToDomain([]ai.Message{source}, "run-1")
	content := domain[0].Payload.Content
	if len(content) != 2 || content[0].Type != conversationentity.ContentTypeText || content[0].Text != "看图" {
		t.Fatalf("payload content = %#v", content)
	}
	if content[1].Type != conversationentity.ContentTypeImage || content[1].Image == nil ||
		content[1].Image.URL != "https://example.com/a.png" {
		t.Fatalf("image block = %#v", content[1])
	}
}

func TestMessagesToHistoryRestoresImageURLs(t *testing.T) {
	messages := []*conversationentity.Message{{
		ID:   "customer-1",
		Role: conversationentity.RoleUser,
		Payload: conversationentity.MessagePayload{Content: []conversationentity.ContentBlock{
			{Type: conversationentity.ContentTypeText, Text: "看图"},
			{Type: conversationentity.ContentTypeImage, Image: &conversationentity.ImageContent{URL: "https://example.com/a.png"}},
			{Type: conversationentity.ContentTypeImage, Image: &conversationentity.ImageContent{URL: "https://example.com/b.png"}},
		}},
	}}

	got, err := messagesToHistory(messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("history length = %d, want 1", len(got))
	}
	message := got[0]
	if message.Content != "看图" || message.SenderType != "customer" {
		t.Fatalf("history message = %#v", message)
	}
	if len(message.ImageURLs) != 2 || message.ImageURLs[0] != "https://example.com/a.png" || message.ImageURLs[1] != "https://example.com/b.png" {
		t.Fatalf("ImageURLs = %v, want both URLs in order", message.ImageURLs)
	}
}

func TestMessagesToHistoryRejectsNonCanonicalOrder(t *testing.T) {
	tests := []struct {
		name    string
		content []conversationentity.ContentBlock
		want    string
	}{
		{
			name: "text after image",
			content: []conversationentity.ContentBlock{
				{Type: conversationentity.ContentTypeText, Text: "看图"},
				{Type: conversationentity.ContentTypeImage, Image: &conversationentity.ImageContent{URL: "https://example.com/a.png"}},
				{Type: conversationentity.ContentTypeText, Text: "再看"},
			},
			want: "canonical order",
		},
		{
			name: "multiple text blocks",
			content: []conversationentity.ContentBlock{
				{Type: conversationentity.ContentTypeText, Text: "第一段"},
				{Type: conversationentity.ContentTypeText, Text: "第二段"},
			},
			want: "multiple text blocks",
		},
		{
			name: "image without url",
			content: []conversationentity.ContentBlock{
				{Type: conversationentity.ContentTypeImage},
			},
			want: "image url",
		},
		{
			name: "unsupported type",
			content: []conversationentity.ContentBlock{
				{Type: "audio", Text: "voice"},
			},
			want: "unsupported content type",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messages := []*conversationentity.Message{{
				ID:      "customer-1",
				Role:    conversationentity.RoleUser,
				Payload: conversationentity.MessagePayload{Content: test.content},
			}}
			_, err := messagesToHistory(messages)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("messagesToHistory() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMessagePayloadImageJSONRoundTrip(t *testing.T) {
	payload := conversationentity.MessagePayload{Content: []conversationentity.ContentBlock{
		{Type: conversationentity.ContentTypeText, Text: "看图"},
		{Type: conversationentity.ContentTypeImage, Image: &conversationentity.ImageContent{URL: "https://example.com/a.png"}},
	}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"url":"https://example.com/a.png"`) {
		t.Fatalf("encoded payload missing image url: %s", encoded)
	}
	var decoded conversationentity.MessagePayload
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, payload) {
		t.Fatalf("round trip = %#v, want %#v", decoded, payload)
	}

	// 旧 payload 无 image 字段，反序列化零影响。
	legacy := []byte(`{"content":[{"type":"text","text":"旧消息"}]}`)
	var legacyPayload conversationentity.MessagePayload
	if err := json.Unmarshal(legacy, &legacyPayload); err != nil {
		t.Fatal(err)
	}
	if len(legacyPayload.Content) != 1 || legacyPayload.Content[0].Image != nil {
		t.Fatalf("legacy payload decode = %#v", legacyPayload)
	}
}

func historyTextPayload(content string) conversationentity.MessagePayload {
	return conversationentity.MessagePayload{Content: []conversationentity.ContentBlock{{
		Type: conversationentity.ContentTypeText,
		Text: content,
	}}}
}

func TestInvocationDomainMappingPreservesUsage(t *testing.T) {
	want := []governor.Invocation{{
		Sequence: 2,
		Phase:    governor.PhaseAction,
		Usage: ai.Usage{
			InputTokens: 120, OutputTokens: 30,
			InputPriceUSDPerMillionTokens: 0.15, OutputPriceUSDPerMillionTokens: 0.60,
			CostUSD: 0.000036, LatencyMS: 245, PlatformID: "zhipu", Model: "glm-4.5-air",
		},
	}}

	got := invocationsToDomain(want, "run-1", "")
	if len(got) != 1 || got[0].Sequence != 2 || got[0].Phase != "action" ||
		got[0].PlatformID != "zhipu" || got[0].CostUSD != 0.000036 || got[0].RunID != "run-1" {
		t.Fatalf("mapped invocations = %#v", got)
	}
}
