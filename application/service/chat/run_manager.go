package chat

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

const runEventQueueSize = 64

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
		s == nil || s.repository == nil || s.ids == nil || s.runner == nil {
		return nil, commonerrors.ErrInvalidParam
	}
	_, found, err := s.repository.FindByUserIDAndConversationID(ctx, userID, conversationID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, commonerrors.ErrNotFound
	}

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
	go s.executeRun(runCtx, key, userID, conversationID, runID, content, events)
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
	key, userID, conversationID, runID, content string,
	events chan vo.RunEventVO,
) {
	defer close(events)
	defer s.releaseRun(key, runID)
	reporter := newRunReporter(runID, events)
	_, err := s.runner.Run(ctx, conversation.RunRequest{
		UserID: userID, ConversationID: conversationID, RunID: runID,
		Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock(content)}},
	}, reporter)
	if err == nil {
		err = s.repository.RenameIfUntitled(ctx, userID, conversationID, firstRunes(content, 60))
	}
	if err != nil {
		sendTerminalEvent(ctx, events, vo.RunEventVO{
			Type: vo.RunEventRunFailed, RunID: runID, Error: runErrorVO(err),
		})
		return
	}
	sendTerminalEvent(ctx, events, vo.RunEventVO{Type: vo.RunEventRunCompleted, RunID: runID})
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

func runErrorVO(err error) *vo.RunErrorVO {
	if errors.Is(err, context.Canceled) {
		return &vo.RunErrorVO{Code: commonerrors.ErrConflict.Code(), Message: "run canceled"}
	}
	if codeErr, ok := err.(commonerrors.CodeError); ok {
		return &vo.RunErrorVO{Code: codeErr.Code(), Message: codeErr.Message()}
	}
	return &vo.RunErrorVO{Code: commonerrors.ErrInternal.Code(), Message: "run failed"}
}

var _ pi.Reporter = (*runReporter)(nil)
