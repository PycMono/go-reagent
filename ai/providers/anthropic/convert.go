package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/ai"
	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
)

func toClaudeMessages(messages []ai.Message) ([]anthropicsdk.MessageParam, []anthropicsdk.TextBlockParam, error) {
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

func toClaudeTools(definitions []ai.ToolDefinition) ([]anthropicsdk.ToolUnionParam, error) {
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

func schemaObject(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	if object, ok := value.(map[string]any); ok {
		return object, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, fmt.Errorf("JSON schema must be an object")
	}
	return object, nil
}
