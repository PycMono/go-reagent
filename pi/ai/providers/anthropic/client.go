package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PycMono/go-reagent/pi/ai"
	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Client adapts the Anthropic Messages protocol.
type Client struct {
	client anthropicsdk.Client
	model  string
	name   string
}

var _ ai.Client = (*Client)(nil)

// New creates an Anthropic-compatible client from one normalized platform profile.
func New(config ai.PlatformConfig) ai.Client {
	return &Client{
		client: anthropicsdk.NewClient(
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
	messages, system, err := toClaudeMessages(msgs)
	if err != nil {
		return nil, ai.WrapGeneration("anthropic generate", fmt.Errorf("%s 消息转换失败: %w", p.name, err))
	}
	tools, err := toClaudeTools(availableTools)
	if err != nil {
		return nil, ai.WrapGeneration("anthropic generate", fmt.Errorf("%s 工具定义转换失败: %w", p.name, err))
	}

	params := anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(p.model),
		MaxTokens: 4096,
		Messages:  messages,
		System:    system,
	}
	if len(tools) > 0 {
		params.Tools = tools
	}

	response, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, ai.WrapGeneration("anthropic generate", fmt.Errorf("%s API 请求失败: %w", p.name, err))
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
