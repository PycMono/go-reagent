package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/ai"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// OpenAIProvider 适配 OpenAI Chat Completions 兼容协议。
type OpenAIProvider struct {
	client openai.Client
	model  string
	name   string
}

var _ LLMProvider = (*OpenAIProvider)(nil)

func newOpenAICompatibleProvider(apiKey, baseURL, model, name string) *OpenAIProvider {
	return &OpenAIProvider{
		client: openai.NewClient(
			option.WithAPIKey(apiKey),
			option.WithBaseURL(baseURL),
		),
		model: model,
		name:  name,
	}
}

func (p *OpenAIProvider) Generate(
	ctx context.Context,
	msgs []ai.Message,
	availableTools []ai.ToolDefinition,
) (*ai.Message, error) {
	openAIMessages, err := toOpenAIMessages(msgs)
	if err != nil {
		return nil, fmt.Errorf("%s 消息转换失败: %w", p.name, err)
	}
	openAITools, err := toOpenAITools(availableTools)
	if err != nil {
		return nil, fmt.Errorf("%s 工具定义转换失败: %w", p.name, err)
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(p.model),
		Messages: openAIMessages,
	}
	if len(openAITools) > 0 {
		params.Tools = openAITools
	}

	response, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("%s API 请求失败: %w", p.name, err)
	}
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("%s API 返回空 choices", p.name)
	}

	message := response.Choices[0].Message
	result := &ai.Message{
		Role: ai.RoleAssistant,
	}
	if message.Content != "" {
		result.Content = []ai.ContentBlock{ai.TextBlock(message.Content)}
	}
	for _, toolCall := range message.ToolCalls {
		if toolCall.Type != "function" {
			continue
		}
		result.ToolCalls = append(result.ToolCalls, ai.ToolCall{
			ID:        toolCall.ID,
			Name:      toolCall.Function.Name,
			Arguments: json.RawMessage(toolCall.Function.Arguments),
		})
	}

	return result, nil
}

func toOpenAIMessages(messages []ai.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, message := range messages {
		text, err := ai.TextContent(message.Content)
		if err != nil {
			return nil, fmt.Errorf("message content: %w", err)
		}
		switch message.Role {
		case ai.RoleSystem:
			result = append(result, openai.SystemMessage(text))
		case ai.RoleUser:
			result = append(result, openai.UserMessage(text))
		case ai.RoleTool:
			if message.ToolCallID == "" {
				return nil, errors.New("tool message requires tool_call_id")
			}
			result = append(result, openai.ToolMessage(text, message.ToolCallID))
		case ai.RoleAssistant:
			if text == "" && len(message.ToolCalls) == 0 {
				return nil, errors.New("assistant message contains no content or tool calls")
			}
			assistant := openai.ChatCompletionAssistantMessageParam{}
			if text != "" {
				assistant.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(text),
				}
			}
			for _, toolCall := range message.ToolCalls {
				assistant.ToolCalls = append(assistant.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: toolCall.ID,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      toolCall.Name,
							Arguments: string(toolCall.Arguments),
						},
					},
				})
			}
			result = append(result, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
		default:
			return nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
	}
	return result, nil
}

func toOpenAITools(definitions []ai.ToolDefinition) ([]openai.ChatCompletionToolUnionParam, error) {
	result := make([]openai.ChatCompletionToolUnionParam, 0, len(definitions))
	for _, definition := range definitions {
		parameters, err := schemaObject(definition.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("tool %q input schema: %w", definition.Name, err)
		}
		result = append(result, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        definition.Name,
			Description: openai.String(definition.Description),
			Parameters:  shared.FunctionParameters(parameters),
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
