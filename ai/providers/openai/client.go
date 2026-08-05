package openai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PycMono/go-reagent/ai"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// Client adapts the OpenAI Chat Completions protocol.
type Client struct {
	client openaisdk.Client
	model  string
	name   string
}

var _ ai.Client = (*Client)(nil)

// New creates an OpenAI-compatible client from one normalized platform profile.
func New(config ai.PlatformConfig) ai.Client {
	return &Client{
		client: openaisdk.NewClient(
			option.WithAPIKey(config.APIKey),
			option.WithBaseURL(config.BaseURL),
		),
		model: config.Model,
		name:  config.ID,
	}
}

func (p *Client) Generate(
	ctx context.Context,
	msgs []ai.Message,
	availableTools []ai.ToolDefinition,
) (*ai.Message, error) {
	openAIMessages, err := toOpenAIMessages(msgs)
	if err != nil {
		return nil, ai.WrapGeneration("openai generate", fmt.Errorf("%s 消息转换失败: %w", p.name, err))
	}
	openAITools, err := toOpenAITools(availableTools)
	if err != nil {
		return nil, ai.WrapGeneration("openai generate", fmt.Errorf("%s 工具定义转换失败: %w", p.name, err))
	}

	params := openaisdk.ChatCompletionNewParams{
		Model:    shared.ChatModel(p.model),
		Messages: openAIMessages,
	}
	if len(openAITools) > 0 {
		params.Tools = openAITools
	}

	response, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, ai.WrapGeneration("openai generate", fmt.Errorf("%s API 请求失败: %w", p.name, err))
	}
	if len(response.Choices) == 0 {
		return nil, ai.WrapGeneration("openai generate", fmt.Errorf("%s API 返回空 choices", p.name))
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
