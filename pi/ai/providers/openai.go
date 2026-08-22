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
	"github.com/openai/openai-go/v3/packages/respjson"
	openaisstream "github.com/openai/openai-go/v3/packages/ssestream"
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
			option.WithMaxRetries(0),
		),
		model: config.Model,
		name:  config.ID,
	}
}

func (p *OpenAIImpl) Stream(
	ctx context.Context,
	msgs []ai.Message,
	availableTools []ai.ToolDefinition,
) ai.Stream {
	openAIMessages, err := toOpenAIMessages(msgs)
	if err != nil {
		return newFailedStream(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "openai stream", fmt.Errorf("%s 消息转换失败: %w", p.name, err)))
	}
	openAITools, err := toOpenAITools(availableTools)
	if err != nil {
		return newFailedStream(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "openai stream", fmt.Errorf("%s 工具定义转换失败: %w", p.name, err)))
	}

	params := openaisdk.ChatCompletionNewParams{
		Model:         p.model,
		Messages:      openAIMessages,
		StreamOptions: openaisdk.ChatCompletionStreamOptionsParam{IncludeUsage: openaisdk.Bool(true)},
	}
	if len(openAITools) > 0 {
		params.Tools = openAITools
	}

	return &openAIStream{provider: p, stream: p.client.Chat.Completions.NewStreaming(ctx, params)}
}

type openAIStream struct {
	streamState
	provider    *OpenAIImpl
	stream      *openaisstream.Stream[openaisdk.ChatCompletionChunk]
	accumulator openaisdk.ChatCompletionAccumulator
	pending     []ai.StreamEvent
	usageSeen   bool
}

func (s *openAIStream) Next() bool {
	if len(s.pending) > 0 {
		s.current = s.pending[0]
		s.pending = s.pending[1:]
		return true
	}
	if s.terminal {
		return false
	}
	if s.start() {
		return true
	}
	if s.stream == nil {
		return s.fail(s.err)
	}

	for s.stream.Next() {
		chunk := s.stream.Current()
		if !s.accumulator.AddChunk(chunk) {
			return s.fail(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "openai stream", errors.New("流式响应拼接失败")))
		}
		if chunk.JSON.Usage.Valid() && chunk.Usage.JSON.PromptTokens.Valid() &&
			chunk.Usage.JSON.CompletionTokens.Valid() {
			s.usageSeen = true
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				s.pending = append(s.pending, ai.StreamEvent{
					Type: ai.StreamEventTextDelta, TextDelta: choice.Delta.Content,
				})
			}
			for _, call := range choice.Delta.ToolCalls {
				s.pending = append(s.pending, ai.StreamEvent{
					Type: ai.StreamEventToolCallDelta,
					ToolCallDelta: &ai.ToolCallDelta{
						Index: int(call.Index), IDDelta: call.ID,
						NameDelta: call.Function.Name, ArgumentsDelta: call.Function.Arguments,
					},
				})
			}
		}
		if len(s.pending) > 0 {
			s.current = s.pending[0]
			s.pending = s.pending[1:]
			return true
		}
	}

	if err := s.stream.Err(); err != nil {
		return s.fail(s.provider.classifyError(err))
	}
	if err := s.finish(); err != nil {
		return s.fail(err)
	}
	return s.done()
}

func (s *openAIStream) Close() error {
	if s.stream == nil {
		return nil
	}
	return s.stream.Close()
}

func (s *openAIStream) finish() error {
	response := s.accumulator.ChatCompletion
	if len(response.Choices) == 0 {
		return pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "openai stream", fmt.Errorf("%s API 返回空 choices", s.provider.name))
	}

	message := response.Choices[0].Message
	result := &ai.Message{
		Role:         ai.RoleAssistant,
		FinishReason: openAIFinishReason(response.Choices[0].FinishReason),
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
	if s.usageSeen {
		result.Usage = mapOpenAIUsage(response.Usage)
	}
	s.result = result
	return nil
}

func openAIFinishReason(reason string) ai.FinishReason {
	switch reason {
	case "tool_calls", "function_call":
		return ai.FinishReasonToolUse
	case "length":
		return ai.FinishReasonLength
	default:
		return ai.FinishReasonStop
	}
}

func (p *OpenAIImpl) classifyError(err error) error {
	info := providerErrorInfo{err: err}
	var apiErr *openaisdk.Error
	if errors.As(err, &apiErr) {
		info.statusCode = apiErr.StatusCode
		info.providerCode = apiErr.Code
		info.contextOverflow = apiErr.Code == "context_length_exceeded"
		info.quotaExceeded = apiErr.Code == "insufficient_quota"
	}
	return classifyError(info)
}

func toOpenAIMessages(messages []ai.Message) ([]openaisdk.ChatCompletionMessageParamUnion, error) {
	normalized, err := normalizeMessages(messages)
	if err != nil {
		return nil, err
	}
	result := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(normalized))
	for _, message := range normalized {
		switch message.role {
		case ai.RoleSystem:
			result = append(result, openaisdk.SystemMessage(message.text))
		case ai.RoleUser:
			result = append(result, openaisdk.UserMessage(message.text))
		case ai.RoleTool:
			result = append(result, openaisdk.ToolMessage(message.text, message.toolCallID))
		case ai.RoleAssistant:
			assistant := openaisdk.ChatCompletionAssistantMessageParam{}
			if message.text != "" {
				assistant.Content = openaisdk.ChatCompletionAssistantMessageParamContentUnion{OfString: openaisdk.String(message.text)}
			}
			for _, toolCall := range message.toolCalls {
				assistant.ToolCalls = append(assistant.ToolCalls, openaisdk.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openaisdk.ChatCompletionMessageFunctionToolCallParam{
						ID: toolCall.id,
						Function: openaisdk.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name: toolCall.name, Arguments: string(toolCall.arguments),
						},
					},
				})
			}
			result = append(result, openaisdk.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
		}
	}
	return result, nil
}

func toOpenAITools(definitions []ai.ToolDefinition) ([]openaisdk.ChatCompletionToolUnionParam, error) {
	normalized, err := normalizeToolDefinitions(definitions)
	if err != nil {
		return nil, err
	}
	result := make([]openaisdk.ChatCompletionToolUnionParam, 0, len(normalized))
	for _, definition := range normalized {
		result = append(result, openaisdk.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name: definition.name, Description: openaisdk.String(definition.description), Parameters: shared.FunctionParameters(definition.inputSchema),
		}))
	}
	return result, nil
}

// mapOpenAIUsage 按 §9.2 归一化 OpenAI 兼容协议的 Usage：
// cached_tokens → CacheReadTokens；reasoning_tokens → ReasoningTokens；
// InputTokens = prompt_tokens。DeepSeek 的 prompt_cache_hit_tokens 是非标
// 字段，经 ExtraFields 读取；其 prompt_tokens 本身即 hit + miss 总量。
// 字段缺失时对应分项为 0，总量与分项必须一致由 §9.1 校验兜底。
func mapOpenAIUsage(usage openaisdk.CompletionUsage) *ai.Usage {
	mapped := &ai.Usage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}
	if usage.JSON.PromptTokensDetails.Valid() {
		mapped.CacheReadTokens = usage.PromptTokensDetails.CachedTokens
	}
	if usage.JSON.CompletionTokensDetails.Valid() {
		mapped.ReasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
	}
	if hit := extraFieldInt64(usage.JSON.ExtraFields, "prompt_cache_hit_tokens"); hit > 0 {
		mapped.CacheReadTokens = hit
	}
	return mapped
}

// extraFieldInt64 读取 Provider 非标 Usage 字段（如 DeepSeek 的
// prompt_cache_hit_tokens）。ExtraFields 条目对未知字段标记为 invalid，
// 因此不能用 Field.Valid()，只按 Raw() 判空并解析；缺失、null 或非数字
// 时返回 0。
func extraFieldInt64(fields map[string]respjson.Field, name string) int64 {
	field, ok := fields[name]
	if !ok {
		return 0
	}
	raw := field.Raw()
	if raw == "" || raw == "null" {
		return 0
	}
	var value int64
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return 0
	}
	return value
}
