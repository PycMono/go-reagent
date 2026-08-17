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
		return newFailedAnthropicStream(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "anthropic stream", fmt.Errorf("%s 消息转换失败: %w", p.name, err)))
	}
	tools, err := toAnthropicTools(availableTools)
	if err != nil {
		return newFailedAnthropicStream(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "anthropic stream", fmt.Errorf("%s 工具定义转换失败: %w", p.name, err)))
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
	provider *AnthropicImpl
	stream   *anthropicstream.Stream[anthropicsdk.MessageStreamEventUnion]
	message  anthropicsdk.Message
	current  ai.StreamEvent
	started  bool
	terminal bool
	result   *ai.Message
	err      error
}

func newFailedAnthropicStream(err error) ai.Stream {
	return &anthropicStream{err: err}
}

func (s *anthropicStream) Next() bool {
	if s.terminal {
		return false
	}
	if !s.started {
		s.started = true
		s.current = ai.StreamEvent{Type: ai.StreamEventStart}
		return true
	}
	if s.stream == nil {
		s.terminal = true
		s.current = ai.StreamEvent{Type: ai.StreamEventError}
		return true
	}

	for s.stream.Next() {
		event := s.stream.Current()
		if err := s.message.Accumulate(event); err != nil {
			s.err = pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "anthropic stream", err)
			s.terminal = true
			s.current = ai.StreamEvent{Type: ai.StreamEventError}
			return true
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
				s.err = err
				s.terminal = true
				s.current = ai.StreamEvent{Type: ai.StreamEventError}
				return true
			}
			s.terminal = true
			s.current = ai.StreamEvent{Type: ai.StreamEventDone}
			return true
		}
	}

	if err := s.stream.Err(); err != nil {
		s.err = s.provider.classifyError(err)
		s.terminal = true
		s.current = ai.StreamEvent{Type: ai.StreamEventError}
		return true
	}
	if err := s.finish(); err != nil {
		s.err = err
		s.terminal = true
		s.current = ai.StreamEvent{Type: ai.StreamEventError}
		return true
	}
	s.terminal = true
	s.current = ai.StreamEvent{Type: ai.StreamEventDone}
	return true
}

func (s *anthropicStream) Current() ai.StreamEvent { return s.current }

func (s *anthropicStream) Result() (*ai.Message, error) { return s.result, s.err }

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
	result := make([]anthropicsdk.MessageParam, 0, len(messages))
	var system []anthropicsdk.TextBlockParam
	for _, message := range messages {
		text, err := ai.TextContent(message.Content)
		if err != nil {
			return nil, nil, fmt.Errorf("message content: %w", err)
		}
		switch message.Role {
		case ai.RoleSystem:
			system = append(system, anthropicsdk.TextBlockParam{Text: text})
		case ai.RoleUser:
			result = append(result, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(text)))
		case ai.RoleTool:
			if message.ToolCallID == "" {
				return nil, nil, errors.New("tool message requires tool_call_id")
			}
			result = append(result, anthropicsdk.NewUserMessage(
				anthropicsdk.NewToolResultBlock(message.ToolCallID, text, message.IsError),
			))
		case ai.RoleAssistant:
			if text == "" && len(message.ToolCalls) == 0 {
				return nil, nil, errors.New("assistant message contains no content or tool calls")
			}
			var blocks []anthropicsdk.ContentBlockParamUnion
			if text != "" {
				blocks = append(blocks, anthropicsdk.NewTextBlock(text))
			}
			for _, toolCall := range message.ToolCalls {
				var input any
				if err := json.Unmarshal(toolCall.Arguments, &input); err != nil {
					return nil, nil, fmt.Errorf("tool call %q arguments: %w", toolCall.ID, err)
				}
				blocks = append(blocks, anthropicsdk.NewToolUseBlock(toolCall.ID, input, toolCall.Name))
			}
			result = append(result, anthropicsdk.NewAssistantMessage(blocks...))
		default:
			return nil, nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
	}
	return result, system, nil
}

func toAnthropicTools(definitions []ai.ToolDefinition) ([]anthropicsdk.ToolUnionParam, error) {
	result := make([]anthropicsdk.ToolUnionParam, 0, len(definitions))
	for _, definition := range definitions {
		object, err := schemaObject(definition.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("tool %q input schema: %w", definition.Name, err)
		}
		var properties any
		var required []string
		extraFields := make(map[string]any)
		for key, value := range object {
			switch key {
			case "type":
				if value != "object" {
					return nil, fmt.Errorf("tool %q input schema type must be object", definition.Name)
				}
			case "properties":
				properties = value
			case "required":
				required, err = stringValues(value)
				if err != nil {
					return nil, fmt.Errorf("tool %q required: %w", definition.Name, err)
				}
			default:
				extraFields[key] = value
			}
		}
		tool := anthropicsdk.ToolParam{
			Name: definition.Name, Description: anthropicsdk.String(definition.Description),
			InputSchema: anthropicsdk.ToolInputSchemaParam{Properties: properties, Required: required, ExtraFields: extraFields},
		}
		result = append(result, anthropicsdk.ToolUnionParam{OfTool: &tool})
	}
	return result, nil
}

func stringValues(value any) ([]string, error) {
	switch values := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return values, nil
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("must contain only strings")
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("must be an array of strings")
	}
}
