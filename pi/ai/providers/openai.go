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
		return newFailedOpenAIStream(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "openai stream", fmt.Errorf("%s 消息转换失败: %w", p.name, err)))
	}
	openAITools, err := toOpenAITools(availableTools)
	if err != nil {
		return newFailedOpenAIStream(pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "openai stream", fmt.Errorf("%s 工具定义转换失败: %w", p.name, err)))
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
	provider    *OpenAIImpl
	stream      *openaisstream.Stream[openaisdk.ChatCompletionChunk]
	accumulator openaisdk.ChatCompletionAccumulator
	current     ai.StreamEvent
	pending     []ai.StreamEvent
	started     bool
	terminal    bool
	usageSeen   bool
	result      *ai.Message
	err         error
}

func newFailedOpenAIStream(err error) ai.Stream {
	return &openAIStream{err: err}
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
	if !s.started {
		s.started = true
		s.current = ai.StreamEvent{Type: ai.StreamEventStart}
		return true
	}
	if s.stream == nil {
		s.terminal = true
		s.current = ai.StreamEvent{Type: ai.StreamEventError}
		return true
	}

	for s.stream.Next() {
		chunk := s.stream.Current()
		if !s.accumulator.AddChunk(chunk) {
			s.err = pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "openai stream", errors.New("流式响应拼接失败"))
			s.terminal = true
			s.current = ai.StreamEvent{Type: ai.StreamEventError}
			return true
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
		s.err = s.provider.classifyError(err)
		s.terminal = true
		s.current = ai.StreamEvent{Type: ai.StreamEventError}
		return true
	}
	if err := s.finish(); err != nil {
		s.err = err
		s.terminal = true
		s.current = ai.StreamEvent{Type: ai.StreamEventError}
		return true
	}
	s.terminal = true
	s.current = ai.StreamEvent{Type: ai.StreamEventDone}
	return true
}

func (s *openAIStream) Current() ai.StreamEvent { return s.current }

func (s *openAIStream) Result() (*ai.Message, error) { return s.result, s.err }

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
		result.Usage = &ai.Usage{
			InputTokens:  response.Usage.PromptTokens,
			OutputTokens: response.Usage.CompletionTokens,
		}
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
