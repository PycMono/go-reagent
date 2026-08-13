package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// OpenAIImpl adapts the OpenAI Chat Completions protocol.
type OpenAIImpl struct {
	client openaisdk.Client
	model  string
	name   string
}

// NewOpenAi creates an OpenAI-compatible provider from one normalized platform profile.
func NewOpenAi(config Options) ai.Provider {
	return &OpenAIImpl{
		client: openaisdk.NewClient(
			option.WithAPIKey(config.APIKey),
			option.WithBaseURL(config.BaseURL),
		),
		model: config.Model,
		name:  config.ID,
	}
}

func (p *OpenAIImpl) Generate(
	ctx context.Context,
	msgs []ai.Message,
	availableTools []ai.ToolDefinition,
) (*ai.Message, error) {
	openAIMessages, err := toOpenAIMessages(msgs)
	if err != nil {
		return nil, pierrors.WrapGeneration("openai generate", fmt.Errorf("%s 消息转换失败: %w", p.name, err))
	}
	openAITools, err := toOpenAITools(availableTools)
	if err != nil {
		return nil, pierrors.WrapGeneration("openai generate", fmt.Errorf("%s 工具定义转换失败: %w", p.name, err))
	}

	params := openaisdk.ChatCompletionNewParams{
		Model:    p.model,
		Messages: openAIMessages,
	}
	if len(openAITools) > 0 {
		params.Tools = openAITools
	}

	response, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, pierrors.WrapGeneration("openai generate", fmt.Errorf("%s API 请求失败: %w", p.name, err))
	}
	if len(response.Choices) == 0 {
		return nil, pierrors.WrapGeneration("openai generate", fmt.Errorf("%s API 返回空 choices", p.name))
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
	if response.JSON.Usage.Valid() && response.Usage.JSON.PromptTokens.Valid() &&
		response.Usage.JSON.CompletionTokens.Valid() {
		result.Usage = &ai.Usage{
			InputTokens:  response.Usage.PromptTokens,
			OutputTokens: response.Usage.CompletionTokens,
		}
	}

	return result, nil
}

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
