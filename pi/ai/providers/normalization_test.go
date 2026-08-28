package providers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

func TestProviderMessageConvertersRejectInvalidToolCallArguments(t *testing.T) {
	messages := []ai.Message{{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{{
			ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"broken"`),
		}},
	}}

	tests := []struct {
		name    string
		convert func() error
	}{
		{
			name: "OpenAI",
			convert: func() error {
				_, err := toOpenAIMessages(messages, false)
				return err
			},
		},
		{
			name: "Anthropic",
			convert: func() error {
				_, _, err := toAnthropicMessages(messages, false)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.convert()
			if err == nil || !strings.Contains(err.Error(), `tool call "call-1" arguments`) {
				t.Fatalf("convert() error = %v, want invalid tool call arguments", err)
			}
		})
	}
}

func TestProviderToolConvertersRejectNonObjectInputSchema(t *testing.T) {
	definitions := []ai.ToolDefinition{{
		Name: "lookup", InputSchema: map[string]any{"type": "string"},
	}}

	tests := []struct {
		name    string
		convert func() error
	}{
		{
			name: "OpenAI",
			convert: func() error {
				_, err := toOpenAITools(definitions)
				return err
			},
		},
		{
			name: "Anthropic",
			convert: func() error {
				_, err := toAnthropicTools(definitions)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.convert()
			if err == nil || !strings.Contains(err.Error(), `tool "lookup" input schema type must be object`) {
				t.Fatalf("convert() error = %v, want non-object schema error", err)
			}
		})
	}
}

func TestNormalizeMessagesUserBlocksPreserveOrder(t *testing.T) {
	messages := []ai.Message{{
		Role: ai.RoleUser,
		Content: []ai.ContentBlock{
			ai.TextBlock("看这张图"),
			ai.ImageBlock("https://example.com/a.png"),
			ai.TextBlock("和这张"),
			ai.ImageBlock("https://example.com/b.png"),
		},
	}}

	normalized, err := normalizeMessages(messages, true)
	if err != nil {
		t.Fatalf("normalizeMessages(vision=true) error = %v", err)
	}
	blocks := normalized[0].blocks
	if len(blocks) != 4 {
		t.Fatalf("blocks = %d, want 4", len(blocks))
	}
	wantTypes := []ai.ContentType{ai.ContentTypeText, ai.ContentTypeImage, ai.ContentTypeText, ai.ContentTypeImage}
	for index, want := range wantTypes {
		if blocks[index].Type != want {
			t.Fatalf("block[%d].Type = %q, want %q", index, blocks[index].Type, want)
		}
	}
	if blocks[1].Image.URL != "https://example.com/a.png" || blocks[3].Image.URL != "https://example.com/b.png" {
		t.Fatalf("image URLs out of order: %q, %q", blocks[1].Image.URL, blocks[3].Image.URL)
	}
}

func TestNormalizeMessagesVisionFalseDegradesImage(t *testing.T) {
	messages := []ai.Message{{
		Role:    ai.RoleUser,
		Content: []ai.ContentBlock{ai.ImageBlock("https://example.com/path/img.png?sig=secret&expires=1#frag")},
	}}

	normalized, err := normalizeMessages(messages, false)
	if err != nil {
		t.Fatalf("normalizeMessages(vision=false) error = %v", err)
	}
	blocks := normalized[0].blocks
	if len(blocks) != 1 || blocks[0].Type != ai.ContentTypeText {
		t.Fatalf("blocks = %+v, want single text placeholder", blocks)
	}
	want := "[图片: https://example.com/path/img.png]"
	if blocks[0].Text != want {
		t.Fatalf("placeholder = %q, want %q", blocks[0].Text, want)
	}
	if strings.Contains(blocks[0].Text, "secret") || strings.Contains(blocks[0].Text, "expires") || strings.Contains(blocks[0].Text, "frag") {
		t.Fatalf("placeholder leaks query/fragment: %q", blocks[0].Text)
	}
}

func TestNormalizeMessagesImagePlaceholderFallsBackToHost(t *testing.T) {
	messages := []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.ImageBlock("https://example.com?q=1")}}}

	normalized, err := normalizeMessages(messages, false)
	if err != nil {
		t.Fatalf("normalizeMessages error = %v", err)
	}
	if want := "[图片: https://example.com]"; normalized[0].blocks[0].Text != want {
		t.Fatalf("placeholder = %q, want %q", normalized[0].blocks[0].Text, want)
	}
}

func TestNormalizeMessagesRejectsImageOutsideUser(t *testing.T) {
	image := []ai.ContentBlock{ai.ImageBlock("https://example.com/a.png")}
	tests := []struct {
		name string
		role ai.Role
	}{
		{name: "system", role: ai.RoleSystem},
		{name: "assistant", role: ai.RoleAssistant},
		{name: "tool", role: ai.RoleTool},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messages := []ai.Message{{Role: test.role, Content: image}}
			if _, err := normalizeMessages(messages, true); err == nil {
				t.Fatalf("normalizeMessages(%s) expected error, got nil", test.role)
			}
		})
	}
}

func TestNormalizeMessagesRejectsInvalidBlock(t *testing.T) {
	tests := []struct {
		name   string
		blocks []ai.ContentBlock
	}{
		{name: "image without url", blocks: []ai.ContentBlock{{Type: ai.ContentTypeImage}}},
		{name: "ftp url", blocks: []ai.ContentBlock{ai.ImageBlock("ftp://example.com/a.png")}},
		{name: "unknown type", blocks: []ai.ContentBlock{{Type: "audio", Text: "hi"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messages := []ai.Message{{Role: ai.RoleUser, Content: test.blocks}}
			if _, err := normalizeMessages(messages, true); err == nil {
				t.Fatalf("normalizeMessages(%s) expected error, got nil", test.name)
			}
		})
	}
}

func TestNormalizeMessagesRejectsEmptyAssistantText(t *testing.T) {
	tests := []struct {
		name    string
		message ai.Message
	}{
		{
			name:    "empty text block without tool calls",
			message: ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("")}},
		},
		{
			name:    "no content",
			message: ai.Message{Role: ai.RoleAssistant},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeMessages([]ai.Message{test.message}, true)
			if err == nil || !strings.Contains(err.Error(), "no content or tool calls") {
				t.Fatalf("normalizeMessages() error = %v, want empty assistant error", err)
			}
		})
	}
}

func TestNormalizeMessagesAcceptsEmptyAssistantTextWithToolCalls(t *testing.T) {
	messages := []ai.Message{{
		Role:      ai.RoleAssistant,
		Content:   []ai.ContentBlock{ai.TextBlock("")},
		ToolCalls: []ai.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)}},
	}}
	if _, err := normalizeMessages(messages, true); err != nil {
		t.Fatalf("normalizeMessages() error = %v, want nil for tool-call-only assistant", err)
	}
}

func TestOpenAIOutboundJSONSnapshot(t *testing.T) {
	image := "https://example.com/a.png?sig=secret"
	messages := []ai.Message{
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("hello")}},
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("看图"), ai.ImageBlock(image)}},
		{Role: ai.RoleTool, ToolCallID: "call-1", Content: []ai.ContentBlock{ai.TextBlock("tool result")}},
	}

	// 纯文本 user 消息保持字符串 content，不扩大兼容风险。
	textOnly, err := toOpenAIMessages(messages[:1], true)
	if err != nil {
		t.Fatal(err)
	}
	textJSON, err := json.Marshal(textOnly)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(textJSON), `"content":"hello"`) {
		t.Fatalf("pure text must stay string content, got: %s", textJSON)
	}

	// 含图消息使用有序 content parts；vision=false 时降级为占位文本。
	withImage, err := toOpenAIMessages(messages[1:2], true)
	if err != nil {
		t.Fatal(err)
	}
	imageJSON, err := json.Marshal(withImage)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(imageJSON), `"type":"image_url"`) ||
		!strings.Contains(string(imageJSON), `"image_url":{"url":"https://example.com/a.png?sig=secret"}`) ||
		!strings.Contains(string(imageJSON), `"text":"看图"`) {
		t.Fatalf("image parts snapshot mismatch, got: %s", imageJSON)
	}

	degraded, err := toOpenAIMessages(messages[1:2], false)
	if err != nil {
		t.Fatal(err)
	}
	degradedJSON, err := json.Marshal(degraded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(degradedJSON), "sig=secret") {
		t.Fatalf("degraded content leaks query params: %s", degradedJSON)
	}
	if !strings.Contains(string(degradedJSON), `"content":"看图[图片: https://example.com/a.png]"`) {
		t.Fatalf("degraded snapshot mismatch, got: %s", degradedJSON)
	}

	// 工具结果保持纯文本。
	toolOnly, err := toOpenAIMessages(messages[2:], true)
	if err != nil {
		t.Fatal(err)
	}
	toolJSON, err := json.Marshal(toolOnly)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(toolJSON), `"content":"tool result"`) {
		t.Fatalf("tool result must stay plain text, got: %s", toolJSON)
	}
}

func TestAnthropicOutboundJSONSnapshot(t *testing.T) {
	messages := []ai.Message{
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("看图"), ai.ImageBlock("https://example.com/a.png?sig=secret")}},
	}
	withImage, _, err := toAnthropicMessages(messages, true)
	if err != nil {
		t.Fatal(err)
	}
	imageJSON, err := json.Marshal(withImage)
	if err != nil {
		t.Fatal(err)
	}
	want := `"url":"https://example.com/a.png?sig=secret"`
	if !strings.Contains(string(imageJSON), `"type":"image"`) || !strings.Contains(string(imageJSON), want) {
		t.Fatalf("anthropic image snapshot mismatch, got: %s", imageJSON)
	}

	degraded, _, err := toAnthropicMessages(messages, false)
	if err != nil {
		t.Fatal(err)
	}
	degradedJSON, err := json.Marshal(degraded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(degradedJSON), "sig=secret") {
		t.Fatalf("degraded content leaks query params: %s", degradedJSON)
	}
	if !strings.Contains(string(degradedJSON), "看图") ||
		!strings.Contains(string(degradedJSON), "[图片: https://example.com/a.png]") {
		t.Fatalf("degraded snapshot mismatch, got: %s", degradedJSON)
	}
}

func TestProviderConversionFailureStreamEmitsStartThenError(t *testing.T) {
	messages := []ai.Message{{Role: ai.Role("unsupported")}}
	tests := []struct {
		name   string
		stream ai.Stream
	}{
		{name: "OpenAI", stream: (&OpenAIImpl{name: "test"}).Stream(context.Background(), messages, nil)},
		{name: "Anthropic", stream: (&AnthropicImpl{name: "test"}).Stream(context.Background(), messages, nil)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var events []ai.StreamEventType
			for test.stream.Next() {
				events = append(events, test.stream.Current().Type)
			}
			if len(events) != 2 || events[0] != ai.StreamEventStart || events[1] != ai.StreamEventError {
				t.Fatalf("events = %v, want [start error]", events)
			}
			message, err := test.stream.Result()
			if message != nil || err == nil {
				t.Fatalf("Result() = (%#v, %v), want (nil, error)", message, err)
			}
			if err := test.stream.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		})
	}
}
