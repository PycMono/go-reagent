package openai

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/ai"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func toOpenAIMessages(messages []ai.Message) ([]openaisdk.ChatCompletionMessageParamUnion, error) {
	result := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, message := range messages {
		text, err := ai.TextContent(message.Content)
		if err != nil {
			return nil, fmt.Errorf("message content: %w", err)
		}
		switch message.Role {
		case ai.RoleSystem:
			result = append(result, openaisdk.SystemMessage(text))
		case ai.RoleUser:
			result = append(result, openaisdk.UserMessage(text))
		case ai.RoleTool:
			if message.ToolCallID == "" {
				return nil, errors.New("tool message requires tool_call_id")
			}
			result = append(result, openaisdk.ToolMessage(text, message.ToolCallID))
		case ai.RoleAssistant:
			if text == "" && len(message.ToolCalls) == 0 {
				return nil, errors.New("assistant message contains no content or tool calls")
			}
			assistant := openaisdk.ChatCompletionAssistantMessageParam{}
			if text != "" {
				assistant.Content = openaisdk.ChatCompletionAssistantMessageParamContentUnion{OfString: openaisdk.String(text)}
			}
			for _, toolCall := range message.ToolCalls {
				assistant.ToolCalls = append(assistant.ToolCalls, openaisdk.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openaisdk.ChatCompletionMessageFunctionToolCallParam{
						ID: toolCall.ID,
						Function: openaisdk.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name: toolCall.Name, Arguments: string(toolCall.Arguments),
						},
					},
				})
			}
			result = append(result, openaisdk.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
		default:
			return nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
	}
	return result, nil
}

func toOpenAITools(definitions []ai.ToolDefinition) ([]openaisdk.ChatCompletionToolUnionParam, error) {
	result := make([]openaisdk.ChatCompletionToolUnionParam, 0, len(definitions))
	for _, definition := range definitions {
		parameters, err := schemaObject(definition.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("tool %q input schema: %w", definition.Name, err)
		}
		result = append(result, openaisdk.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name: definition.Name, Description: openaisdk.String(definition.Description), Parameters: shared.FunctionParameters(parameters),
		}))
	}
	return result, nil
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
