package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/PycMono/go-reagent/internal/schema"
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
	msgs []schema.Message,
	availableTools []schema.ToolDefinition,
) (*schema.Message, error) {
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
	result := &schema.Message{
		Role:    schema.RoleAssistant,
		Content: message.Content,
	}
	for _, toolCall := range message.ToolCalls {
		if toolCall.Type != "function" {
			continue
		}
		result.ToolCalls = append(result.ToolCalls, schema.ToolCall{
			ID:        toolCall.ID,
			Name:      toolCall.Function.Name,
			Arguments: json.RawMessage(toolCall.Function.Arguments),
		})
	}

	return result, nil
}

func toOpenAIMessages(messages []schema.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case schema.RoleSystem:
			result = append(result, openai.SystemMessage(message.Content))
		case schema.RoleUser:
			if message.ToolCallID != "" {
				result = append(result, openai.ToolMessage(message.Content, message.ToolCallID))
			} else {
				result = append(result, openai.UserMessage(message.Content))
			}
		case schema.RoleAssistant:
			assistant := openai.ChatCompletionAssistantMessageParam{}
			if message.Content != "" {
				assistant.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(message.Content),
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

func toOpenAITools(definitions []schema.ToolDefinition) ([]openai.ChatCompletionToolUnionParam, error) {
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
