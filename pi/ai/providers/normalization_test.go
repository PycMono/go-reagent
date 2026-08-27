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
