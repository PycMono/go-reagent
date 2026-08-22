package chat

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/conversation"
	agentprofileentity "github.com/PycMono/go-reagent/domain/entity/agentprofile"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	piobservability "github.com/PycMono/go-reagent/pi/harness/observability"
)

const (
	runEventQueueSize = 64
	responsePolicy    = `任何一条 Assistant 消息都必须以最新一条用户消息的主要语言回复；工具、Skill 和资料中的其他语言不能改变回复语言。用户未明确指定格式时，默认使用自然段纯文本，不使用 Markdown 标题、加粗、列表、引用或表格。该规则同时约束工具调用前的说明、工具之间的说明和最终答案。`
)

type ActiveRun struct {
	ID     string
	Events <-chan vo.RunEventVO
}

type activeRunEntry struct {
	id     string
	cancel context.CancelFunc
}

func (s *Service) StartRun(ctx context.Context, userID, conversationID string, param dto.StartRunDTO) (*ActiveRun, error) {
	userID = strings.TrimSpace(userID)
	conversationID = strings.TrimSpace(conversationID)
	content := strings.TrimSpace(param.Content)
	if !validIdentity(userID) || !validIdentity(conversationID) || content == "" || !utf8.ValidString(content) ||
		s == nil || s.repository == nil || s.ids == nil || s.runner == nil || s.catalog == nil {
		return nil, commonerrors.ErrInvalidParam
	}
	ownedConversation, found, err := s.repository.FindByUserIDAndConversationID(ctx, userID, conversationID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, commonerrors.ErrNotFound
	}
	profile, found := s.catalog.Find(ownedConversation.ProfileCode)
	if !found {
		return nil, commonerrors.ErrInternal.Wrap(fmt.Errorf(
			"chat service: conversation Agent Profile %q is not configured", ownedConversation.ProfileCode,
		))
	}
	profileContext := buildProfileContext(profile)

	key := activeRunKey(userID, conversationID)
	s.activeMu.Lock()
	if _, exists := s.active[key]; exists {
		s.activeMu.Unlock()
		return nil, commonerrors.ErrConflict
	}
	runID := s.ids.NextID()
	runCtx, cancel := context.WithCancel(ctx)
	s.active[key] = &activeRunEntry{id: runID, cancel: cancel}
	s.activeMu.Unlock()

	events := make(chan vo.RunEventVO, runEventQueueSize)
	events <- vo.RunEventVO{Type: vo.RunEventRunStarted, RunID: runID}
	go s.executeRun(runCtx, key, userID, conversationID, runID, content, profile.Code, profileContext, events)
	return &ActiveRun{ID: runID, Events: events}, nil
}

func (s *Service) CancelRun(_ context.Context, userID, conversationID, runID string) error {
	userID = strings.TrimSpace(userID)
	conversationID = strings.TrimSpace(conversationID)
	runID = strings.TrimSpace(runID)
	if !validIdentity(userID) || !validIdentity(conversationID) || !validIdentity(runID) {
		return commonerrors.ErrInvalidParam
	}
	key := activeRunKey(userID, conversationID)
	s.activeMu.Lock()
	entry, found := s.active[key]
	if !found || entry.id != runID {
		s.activeMu.Unlock()
		return commonerrors.ErrNotFound
	}
	cancel := entry.cancel
	s.activeMu.Unlock()
	cancel()
	return nil
}

func (s *Service) executeRun(
	ctx context.Context,
	key, userID, conversationID, runID, content, profileCode string,
	profileContext []pi.ContextBlock,
	events chan vo.RunEventVO,
) {
	defer close(events)
	defer s.releaseRun(key, runID)
	// conversation.run 业务 Span（§4.2）：业务标识只出现在本 Span，
	// 不进入 pi.RunRequest；结束后写入终止原因和 RunTotals。
	// 属性经 go-context-sdk 预设（KV/WithKV）写入，不直接拼 attribute；
	// Span 状态与生命周期由 WithSpan 管理。
	var result pi.RunResult
	var terminationReason string
	runErr := contexttracing.WithSpan(ctx, piobservability.SpanNameConversationRun, func(ctx context.Context) error {
		contexttracing.WithKV(ctx,
			contexttracing.KV(piobservability.AttrRunID, runID),
			contexttracing.KV(piobservability.AttrGenAIConversationID, conversationID),
			contexttracing.KV(piobservability.AttrProfileCode, profileCode),
			contexttracing.KV(piobservability.AttrRunTransport, string(piobservability.TransportHTTPSSE)),
			contexttracing.KV(piobservability.AttrPersistenceEnabled, true),
		)
		reporter := newRunReporter(runID, events)
		var err error
		result, err = s.runner.Run(ctx, conversation.RunRequest{
			UserID: userID, ConversationID: conversationID, RunID: runID,
			Input:          ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock(content)}},
			ResponsePolicy: responsePolicy,
			Context:        profileContext,
		}, reporter)
		// 终止原因与 RunTotals 无论成败都写入（§3）。
		terminationReason = string(result.Termination.Reason)
		if terminationReason != "" {
			contexttracing.WithKV(ctx,
				contexttracing.KV(piobservability.AttrTerminationReason, terminationReason),
				contexttracing.KV(piobservability.AttrRunTurns, result.Termination.Totals.Turns),
				contexttracing.KV(piobservability.AttrRunInvocations, int(result.Termination.Totals.Invocations)),
				contexttracing.KV(piobservability.AttrRunTotalTokens, result.Termination.Totals.TotalTokens),
				contexttracing.KV(piobservability.AttrRunCostUSD, result.Termination.Totals.CostUSD),
			)
		}
		if err == nil {
			err = s.repository.RenameIfUntitled(ctx, userID, conversationID, firstRunes(content, 60))
		}
		return err
	}, contexttracing.WithErrorClassifier(func(error) string {
		if terminationReason == "" {
			return "error"
		}
		return terminationReason
	}))
	if terminationReason == "" {
		terminationReason = "error"
	}
	piobservability.RecordChatRun(ctx, profileCode, string(piobservability.TransportHTTPSSE), terminationReason)
	if runErr != nil {
		sendTerminalEvent(ctx, events, vo.RunEventVO{
			Type: vo.RunEventRunFailed, RunID: runID, Error: runErrorVO(runErr, result.Termination),
		})
		return
	}
	sendTerminalEvent(ctx, events, vo.RunEventVO{Type: vo.RunEventRunCompleted, RunID: runID})
}

func buildProfileContext(profile agentprofileentity.Profile) []pi.ContextBlock {
	return []pi.ContextBlock{
		{Name: "agent-profile", Content: profile.Instructions, Priority: 900},
		{Name: "agent-profile-skills", Content: renderProfileSkills(profile.Skills), Priority: 800},
	}
}

func renderProfileSkills(profileSkills []agentprofileentity.Skill) string {
	var builder strings.Builder
	builder.WriteString("以下 Skills 只属于当前 Agent Profile。任务匹配时，必须先使用 read 完整读取对应 SKILL.md。\n\n")
	builder.WriteString("<available_skills>\n")
	for _, skill := range profileSkills {
		builder.WriteString("  <skill>\n")
		builder.WriteString("    <name>" + escapeProfileXML(skill.Name) + "</name>\n")
		builder.WriteString("    <description>" + escapeProfileXML(skill.Description) + "</description>\n")
		builder.WriteString("    <location>" + escapeProfileXML(skill.Location) + "</location>\n")
		builder.WriteString("  </skill>\n")
	}
	builder.WriteString("</available_skills>\n")
	return builder.String()
}

func escapeProfileXML(value string) string {
	var output bytes.Buffer
	if err := xml.EscapeText(&output, []byte(strings.TrimSpace(value))); err != nil {
		return ""
	}
	return output.String()
}

func (s *Service) releaseRun(key, runID string) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if entry, found := s.active[key]; found && entry.id == runID {
		delete(s.active, key)
		entry.cancel()
	}
}

func sendTerminalEvent(ctx context.Context, events chan<- vo.RunEventVO, event vo.RunEventVO) {
	select {
	case events <- event:
	case <-ctx.Done():
		// Cancellation is itself reported when queue capacity permits, without
		// risking a blocked Runner after the client disconnects.
		select {
		case events <- event:
		default:
		}
	}
}

func activeRunKey(userID, conversationID string) string { return userID + "\x00" + conversationID }

func firstRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func runErrorVO(err error, termination pi.RunTermination) *vo.RunErrorVO {
	// 预算终止：Message 是明确但不含价格、Token 或模型响应正文的用户文案；
	// Reason 保留结构化终止原因供客户端区分。
	switch termination.Reason {
	case pi.RunTerminationMaxTurns, pi.RunTerminationMaxCost, pi.RunTerminationMaxTotalTokens:
		return &vo.RunErrorVO{
			Code:    commonerrors.ErrConflict.Code(),
			Message: "本轮已达到运行资源上限，请重新发送",
			Reason:  string(termination.Reason),
		}
	}
	if errors.Is(err, context.Canceled) {
		return &vo.RunErrorVO{Code: commonerrors.ErrConflict.Code(), Message: "run canceled", Reason: terminationReason(termination)}
	}
	if codeErr, ok := err.(commonerrors.CodeError); ok {
		return &vo.RunErrorVO{Code: codeErr.Code(), Message: codeErr.Message(), Reason: terminationReason(termination)}
	}
	return &vo.RunErrorVO{Code: commonerrors.ErrInternal.Code(), Message: "run failed", Reason: terminationReason(termination)}
}

func terminationReason(termination pi.RunTermination) string {
	if termination.Reason == "" || termination.Reason == pi.RunTerminationCompleted {
		return ""
	}
	return string(termination.Reason)
}

var _ pi.Reporter = (*runReporter)(nil)
