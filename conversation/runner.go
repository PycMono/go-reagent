package conversation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/governor"
	piobservability "github.com/PycMono/go-reagent/pi/harness/observability"
)

func stableErrorCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	}
	var codeErr commonerrors.CodeError
	if errors.As(err, &codeErr) {
		return strconv.Itoa(codeErr.Code())
	}
	return "internal"
}

type runner struct {
	runtime      pi.Runner
	repository   conversationrepo.IConversationRepository
	historyLimit int
	limits       governor.Limits
}

func NewRunner(runtime pi.Runner, repository conversationrepo.IConversationRepository, historyLimit int, limits governor.Limits) Runner {
	return &runner{runtime: runtime, repository: repository, historyLimit: historyLimit, limits: limits}
}

func (r *runner) Run(ctx context.Context, request RunRequest, listener pi.EventListener) (pi.RunResult, error) {
	result := pi.RunResult{}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("conversation runner: run canceled: %w", err)
	}
	userID, conversationID, inputText, imageURLs, err := validateRunRequest(request)
	if err != nil {
		return result, err
	}

	conversation, found, err := r.repository.FindByUserIDAndConversationID(ctx, userID, conversationID)
	if err != nil {
		return result, err
	}
	if !found {
		if err := r.repository.Create(ctx, &conversationentity.Conversation{ConversationID: conversationID, UserID: userID}); err != nil {
			return result, err
		}
		conversation, found, err = r.repository.FindByUserIDAndConversationID(ctx, userID, conversationID)
		if err != nil {
			return result, err
		}
		if !found {
			return result, commonerrors.ErrInternal.Wrap(errors.New("conversation runner: created conversation was not found"))
		}
	}
	var historyMessages []pi.Message
	err = contexttracing.WithSpan(ctx, piobservability.SpanNameConversationLoadHistory, func(loadCtx context.Context) error {
		history, err := r.repository.ListMessagesByConversationID(loadCtx, conversation.ID, r.historyLimit)
		if err != nil {
			return err
		}
		converted, err := messagesToHistory(history)
		if err != nil {
			return err
		}
		historyMessages = converted
		return nil
	}, contexttracing.WithErrorClassifier(stableErrorCode))
	if err != nil {
		return result, err
	}
	runtimeInputText := inputText
	if responsePolicy := strings.TrimSpace(request.ResponsePolicy); responsePolicy != "" {
		runtimeInputText += "\n\n<runtime_response_policy>\n" + responsePolicy + "\n</runtime_response_policy>"
	}
	runtimeResult, runErr := r.runtime.Run(ctx, pi.RunRequest{
		History: historyMessages,
		Input: pi.Message{
			ContentType: "text",
			Content:     runtimeInputText,
			ImageURLs:   imageURLs,
			SenderType:  "customer",
		},
		Context: append([]pi.ContextBlock(nil), request.Context...),
		Limits:  r.limits,
	}, listener)
	if runErr != nil && len(runtimeResult.NewMessages) == 0 && len(runtimeResult.Invocations) == 0 {
		return runtimeResult, runErr
	}

	messages := make([]ai.Message, 0, 1+len(runtimeResult.NewMessages))
	messages = append(messages, cloneMessage(request.Input))
	messages = append(messages, cloneMessages(runtimeResult.NewMessages)...)
	// §10.2：取消后仍须保存终态——使用脱离取消的 3 秒超时 Context 执行
	// AppendTurn；该 Context 仅用于终态持久化。
	persistCtx := ctx
	cancelPersist := func() {}
	if ctx.Err() != nil {
		persistCtx, cancelPersist = context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	}
	defer cancelPersist()
	// persist_turn Span 中原子保存消息与 Invocation（§3）。TraceID 取当前
	// conversation.run 的 SpanContext，无效时写 NULL（§10.1）。
	persistErr := contexttracing.WithSpan(persistCtx, piobservability.SpanNameConversationPersistTurn, func(spanCtx context.Context) error {
		return r.repository.AppendTurn(
			spanCtx,
			userID,
			conversationID,
			conversation.Version,
			messagesToDomain(messages, request.RunID),
			invocationsToDomain(runtimeResult.Invocations, request.RunID, contexttracing.TraceIDFromContext(ctx)),
		)
	}, contexttracing.WithErrorClassifier(stableErrorCode))
	return runtimeResult, errors.Join(runErr, persistErr)
}

func validateRunRequest(request RunRequest) (string, string, string, []string, error) {
	userID := strings.TrimSpace(request.UserID)
	conversationID := strings.TrimSpace(request.ConversationID)
	switch {
	case userID == "":
		return "", "", "", nil, errors.New("conversation runner: user ID is required")
	case conversationID == "":
		return "", "", "", nil, errors.New("conversation runner: conversation ID is required")
	case request.Input.Role != ai.RoleUser:
		return "", "", "", nil, fmt.Errorf("conversation runner: input role must be user, got %q", request.Input.Role)
	}
	inputText, imageURLs, err := canonicalUserContent(request.Input.Content)
	if err != nil {
		return "", "", "", nil, err
	}
	if len(request.Input.ToolCalls) != 0 || request.Input.ToolCallID != "" ||
		request.Input.ToolName != "" || request.Input.IsError {
		return "", "", "", nil, errors.New("conversation runner: input must not contain tool fields")
	}
	return userID, conversationID, inputText, imageURLs, nil
}

// canonicalUserContent 校验 user 输入的规范形态：一个非空 text 块在前，
// 0..N 个 image 块在后；其他形态（无正文、交错混排、image 在前）fail-fast。
// Provider 归一化层保持保序映射，规范形态只是业务链路的入口约束。
func canonicalUserContent(content []ai.ContentBlock) (string, []string, error) {
	if len(content) == 0 {
		return "", nil, errors.New("conversation runner: input content must not be empty")
	}
	if content[0].Type != ai.ContentTypeText {
		return "", nil, fmt.Errorf("conversation runner: input must start with a text block, got %q", content[0].Type)
	}
	if err := content[0].Validate(); err != nil {
		return "", nil, fmt.Errorf("conversation runner: input content: %w", err)
	}
	if strings.TrimSpace(content[0].Text) == "" {
		return "", nil, errors.New("conversation runner: input text block must not be empty")
	}
	var imageURLs []string
	for index, block := range content[1:] {
		if err := block.Validate(); err != nil {
			return "", nil, fmt.Errorf("conversation runner: input content block %d: %w", index+1, err)
		}
		if block.Type != ai.ContentTypeImage {
			return "", nil, fmt.Errorf(
				"conversation runner: only image blocks may follow the input text block, got %q at index %d",
				block.Type, index+1,
			)
		}
		imageURLs = append(imageURLs, block.Image.URL)
	}
	return content[0].Text, imageURLs, nil
}

func cloneMessages(messages []ai.Message) []ai.Message {
	if messages == nil {
		return nil
	}
	cloned := make([]ai.Message, len(messages))
	for index := range messages {
		cloned[index] = cloneMessage(messages[index])
	}
	return cloned
}

func cloneMessage(message ai.Message) ai.Message {
	cloned := message
	cloned.Content = append([]ai.ContentBlock(nil), message.Content...)
	if message.Usage != nil {
		usage := *message.Usage
		cloned.Usage = &usage
	}
	if message.ToolCalls != nil {
		cloned.ToolCalls = make([]ai.ToolCall, len(message.ToolCalls))
		for index, call := range message.ToolCalls {
			cloned.ToolCalls[index] = call
			cloned.ToolCalls[index].Arguments = append([]byte(nil), call.Arguments...)
		}
	}
	return cloned
}
