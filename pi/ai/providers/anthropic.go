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

func (p *AnthropicImpl) Generate(
	ctx context.Context,
	msgs []ai.Message,
	availableTools []ai.ToolDefinition,
) (*ai.Message, error) {
	messages, system, err := toAnthropicMessages(msgs)
	if err != nil {
		return nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "anthropic generate", fmt.Errorf("%s 消息转换失败: %w", p.name, err))
	}
	tools, err := toAnthropicTools(availableTools)
	if err != nil {
		return nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "anthropic generate", fmt.Errorf("%s 工具定义转换失败: %w", p.name, err))
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

	response, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, p.classifyError(err)
	}
	if response.StopReason == anthropicsdk.StopReasonModelContextWindowExceeded {
		return nil, pierrors.Wrap(
			pierrors.ErrorCodeAIContextOverflow,
			"anthropic generate",
			errors.New("model context window exceeded"),
		)
	}

	result := &ai.Message{Role: ai.RoleAssistant}
	for _, block := range response.Content {
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
	if response.JSON.Usage.Valid() && response.Usage.JSON.InputTokens.Valid() &&
		response.Usage.JSON.OutputTokens.Valid() {
		result.Usage = &ai.Usage{
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
		}
	}

	return result, nil
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
