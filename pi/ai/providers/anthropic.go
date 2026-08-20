package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	anthropicstream "github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// AnthropicImpl adapts the Anthropic Messages protocol.
type AnthropicImpl struct {
	client anthropicsdk.Client
	model  string
	name   string
}

// NewAnthropic creates an Anthropic-compatible provider from one normalized platform profile.
func NewAnthropic(config Options) ai.Provider {
	return &AnthropicImpl{
		client: anthropicsdk.NewClient(
			option.WithAPIKey(config.APIKey),
			option.WithBaseURL(config.BaseURL),
			option.WithMaxRetries(0),
		),
		model: config.Model,
		name:  config.ID,
	}
}

func (p *AnthropicImpl) Stream(
	ctx context.Context,
	msgs []ai.Message,
	availableTools []ai.ToolDefinition,
) ai.Stream {
	messages, system, err := toAnthropicMessages(msgs)
	if err != nil {
		return newFailedStream(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "anthropic stream", fmt.Errorf("%s 消息转换失败: %w", p.name, err)))
	}
	tools, err := toAnthropicTools(availableTools)
	if err != nil {
		return newFailedStream(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "anthropic stream", fmt.Errorf("%s 工具定义转换失败: %w", p.name, err)))
	}

	params := anthropicsdk.MessageNewParams{
		Model:     p.model,
		MaxTokens: 4096,
		Messages:  messages,
		System:    system,
	}
	if len(tools) > 0 {
		params.Tools = tools
	}

	return &anthropicStream{provider: p, stream: p.client.Messages.NewStreaming(ctx, params)}
}

type anthropicStream struct {
	streamState
	provider *AnthropicImpl
	stream   *anthropicstream.Stream[anthropicsdk.MessageStreamEventUnion]
	message  anthropicsdk.Message
}

func (s *anthropicStream) Next() bool {
	if s.terminal {
		return false
	}
	if s.start() {
		return true
	}
	if s.stream == nil {
		return s.fail(s.err)
	}

	for s.stream.Next() {
		event := s.stream.Current()
		if err := s.message.Accumulate(event); err != nil {
			return s.fail(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "anthropic stream", err))
		}
		switch current := event.AsAny().(type) {
		case anthropicsdk.ContentBlockStartEvent:
			if current.ContentBlock.Type == "tool_use" {
				s.current = ai.StreamEvent{
					Type: ai.StreamEventToolCallDelta,
					ToolCallDelta: &ai.ToolCallDelta{
						Index: int(current.Index), IDDelta: current.ContentBlock.ID, NameDelta: current.ContentBlock.Name,
					},
				}
				return true
			}
		case anthropicsdk.ContentBlockDeltaEvent:
			switch delta := current.Delta.AsAny().(type) {
			case anthropicsdk.TextDelta:
				if delta.Text != "" {
					s.current = ai.StreamEvent{Type: ai.StreamEventTextDelta, TextDelta: delta.Text}
					return true
				}
			case anthropicsdk.InputJSONDelta:
				if delta.PartialJSON != "" {
					s.current = ai.StreamEvent{
						Type: ai.StreamEventToolCallDelta,
						ToolCallDelta: &ai.ToolCallDelta{
							Index: int(current.Index), ArgumentsDelta: delta.PartialJSON,
						},
					}
					return true
				}
			}
		case anthropicsdk.MessageStopEvent:
			if err := s.finish(); err != nil {
				return s.fail(err)
			}
			return s.done()
		}
	}

	if err := s.stream.Err(); err != nil {
		return s.fail(s.provider.classifyError(err))
	}
	if err := s.finish(); err != nil {
		return s.fail(err)
	}
	return s.done()
}

func (s *anthropicStream) Close() error {
	if s.stream == nil {
		return nil
	}
	return s.stream.Close()
}

func (s *anthropicStream) finish() error {
	if s.message.StopReason == anthropicsdk.StopReasonModelContextWindowExceeded {
		return pierrors.Wrap(
			pierrors.ErrorCodeAIContextOverflow,
			"anthropic stream",
			errors.New("model context window exceeded"),
		)
	}

	result := &ai.Message{Role: ai.RoleAssistant, FinishReason: anthropicFinishReason(s.message.StopReason)}
	for _, block := range s.message.Content {
		switch block.Type {
		case "text":
			result.Content = append(result.Content, ai.TextBlock(block.Text))
		case "tool_use":
			result.ToolCalls = append(result.ToolCalls, ai.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: append(json.RawMessage(nil), block.Input...),
			})
		}
	}
	result.Usage = &ai.Usage{
		InputTokens:  s.message.Usage.InputTokens,
		OutputTokens: s.message.Usage.OutputTokens,
	}
	s.result = result
	return nil
}

func anthropicFinishReason(reason anthropicsdk.StopReason) ai.FinishReason {
	switch reason {
	case anthropicsdk.StopReasonToolUse:
		return ai.FinishReasonToolUse
	case anthropicsdk.StopReasonMaxTokens:
		return ai.FinishReasonLength
	default:
		return ai.FinishReasonStop
	}
}

func (p *AnthropicImpl) classifyError(err error) error {
	info := providerErrorInfo{err: err}
	var apiErr *anthropicsdk.Error
	if errors.As(err, &apiErr) {
		info.statusCode = apiErr.StatusCode
		info.providerCode = string(apiErr.Type())
		switch apiErr.Type() {
		case anthropicsdk.ErrorTypeBillingError:
			info.quotaExceeded = true
		case anthropicsdk.ErrorTypeRateLimitError:
			info.statusCode = http.StatusTooManyRequests
		case anthropicsdk.ErrorTypeTimeoutError:
			info.statusCode = http.StatusRequestTimeout
		case anthropicsdk.ErrorTypeOverloadedError,
			anthropicsdk.ErrorTypeAPIError:
			info.statusCode = http.StatusInternalServerError
		case anthropicsdk.ErrorTypeAuthenticationError:
			info.statusCode = http.StatusUnauthorized
		case anthropicsdk.ErrorTypePermissionError:
			info.statusCode = http.StatusForbidden
		}
	}
	return classifyError(info)
}

func toAnthropicMessages(messages []ai.Message) ([]anthropicsdk.MessageParam, []anthropicsdk.TextBlockParam, error) {
	normalized, err := normalizeMessages(messages)
	if err != nil {
		return nil, nil, err
	}
	result := make([]anthropicsdk.MessageParam, 0, len(normalized))
	var system []anthropicsdk.TextBlockParam
	for _, message := range normalized {
		switch message.role {
		case ai.RoleSystem:
			system = append(system, anthropicsdk.TextBlockParam{Text: message.text})
		case ai.RoleUser:
			result = append(result, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(message.text)))
		case ai.RoleTool:
			result = append(result, anthropicsdk.NewUserMessage(
				anthropicsdk.NewToolResultBlock(message.toolCallID, message.text, message.isError),
			))
		case ai.RoleAssistant:
			var blocks []anthropicsdk.ContentBlockParamUnion
			if message.text != "" {
				blocks = append(blocks, anthropicsdk.NewTextBlock(message.text))
			}
			for _, toolCall := range message.toolCalls {
				blocks = append(blocks, anthropicsdk.NewToolUseBlock(toolCall.id, toolCall.input, toolCall.name))
			}
			result = append(result, anthropicsdk.NewAssistantMessage(blocks...))
		}
	}
	return result, system, nil
}

func toAnthropicTools(definitions []ai.ToolDefinition) ([]anthropicsdk.ToolUnionParam, error) {
	normalized, err := normalizeToolDefinitions(definitions)
	if err != nil {
		return nil, err
	}
	result := make([]anthropicsdk.ToolUnionParam, 0, len(normalized))
	for _, definition := range normalized {
		var properties any
		var required []string
		extraFields := make(map[string]any)
		for key, value := range definition.inputSchema {
			switch key {
			case "type":
			case "properties":
				properties = value
			case "required":
				required, err = stringValues(value)
				if err != nil {
					return nil, fmt.Errorf("tool %q required: %w", definition.name, err)
				}
			default:
				extraFields[key] = value
			}
		}
		tool := anthropicsdk.ToolParam{
			Name: definition.name, Description: anthropicsdk.String(definition.description),
			InputSchema: anthropicsdk.ToolInputSchemaParam{Properties: properties, Required: required, ExtraFields: extraFields},
		}
		result = append(result, anthropicsdk.ToolUnionParam{OfTool: &tool})
	}
	return result, nil
}
