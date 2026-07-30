package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/PycMono/go-reagent/internal/schema"
)

type ClaudeProvider struct {
	client anthropic.Client
	model  string
	name   string
}

var _ LLMProvider = (*ClaudeProvider)(nil)

func newClaudeProvider(apiKey, baseURL, model, name string) *ClaudeProvider {
	return &ClaudeProvider{
		client: anthropic.NewClient(
			option.WithAPIKey(apiKey),
			option.WithBaseURL(baseURL),
		),
		model: model,
		name:  name,
	}
}

func (p *ClaudeProvider) Generate(
	ctx context.Context,
	msgs []schema.Message,
	availableTools []schema.ToolDefinition,
) (*schema.Message, error) {
	messages, system, err := toClaudeMessages(msgs)
	if err != nil {
		return nil, fmt.Errorf("%s 消息转换失败: %w", p.name, err)
	}
	tools, err := toClaudeTools(availableTools)
	if err != nil {
		return nil, fmt.Errorf("%s 工具定义转换失败: %w", p.name, err)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: 4096,
		Messages:  messages,
		System:    system,
	}
	if len(tools) > 0 {
		params.Tools = tools
	}

	response, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("%s API 请求失败: %w", p.name, err)
	}

	result := &schema.Message{Role: schema.RoleAssistant}
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			result.ToolCalls = append(result.ToolCalls, schema.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: append(json.RawMessage(nil), block.Input...),
			})
		}
	}

	return result, nil
}

func toClaudeMessages(messages []schema.Message) (
	[]anthropic.MessageParam,
	[]anthropic.TextBlockParam,
	error,
) {
	result := make([]anthropic.MessageParam, 0, len(messages))
	var system []anthropic.TextBlockParam

	for _, message := range messages {
		switch message.Role {
		case schema.RoleSystem:
			system = append(system, anthropic.TextBlockParam{Text: message.Content})
		case schema.RoleUser:
			if message.ToolCallID != "" {
				result = append(result, anthropic.NewUserMessage(
					anthropic.NewToolResultBlock(message.ToolCallID, message.Content, false),
				))
			} else {
				result = append(result, anthropic.NewUserMessage(
					anthropic.NewTextBlock(message.Content),
				))
			}
		case schema.RoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			if message.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(message.Content))
			}
			for _, toolCall := range message.ToolCalls {
				var input any
				if err := json.Unmarshal(toolCall.Arguments, &input); err != nil {
					return nil, nil, fmt.Errorf("tool call %q arguments: %w", toolCall.ID, err)
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(toolCall.ID, input, toolCall.Name))
			}
			if len(blocks) == 0 {
				return nil, nil, fmt.Errorf("assistant message contains no content or tool calls")
			}
			result = append(result, anthropic.NewAssistantMessage(blocks...))
		default:
			return nil, nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
	}

	return result, system, nil
}

func toClaudeTools(definitions []schema.ToolDefinition) ([]anthropic.ToolUnionParam, error) {
	result := make([]anthropic.ToolUnionParam, 0, len(definitions))
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

		tool := anthropic.ToolParam{
			Name:        definition.Name,
			Description: anthropic.String(definition.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties:  properties,
				Required:    required,
				ExtraFields: extraFields,
			},
		}
		result = append(result, anthropic.ToolUnionParam{OfTool: &tool})
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
