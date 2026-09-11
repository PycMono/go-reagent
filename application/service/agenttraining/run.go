package agenttraining

import (
	"context"
	"errors"
	"github.com/PycMono/go-reagent/application/identity"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/common/dto"
	ce "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	mapping "github.com/PycMono/go-reagent/conversation"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	conversation "github.com/PycMono/go-reagent/domain/entity/conversation"
	repo "github.com/PycMono/go-reagent/domain/repository/agenttraining"
	"github.com/PycMono/go-reagent/pi"
	"strings"
	"time"
)

func (s *Service) Run(ctx context.Context, p identity.Principal, id string, in dto.TrainingRunDTO, listeners ...pi.EventListener) (vo.TrainingSessionVO, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return vo.TrainingSessionVO{}, err
	}
	if !identity.ValidID(in.RunID) || strings.TrimSpace(in.Content) == "" || len(in.Content) > 1<<20 {
		return sessionVO(item), ce.ErrInvalidParam
	}
	input := pi.Message{ContentType: "text", SenderType: "customer", Content: in.Content, ImageURLs: in.ImageURLs}
	if _, err = input.Message2AI(); err != nil {
		return sessionVO(item), ce.ErrInvalidParam
	}
	in.RequestID = in.RunID
	digest, err := requestDigest("run", in)
	if err != nil {
		return sessionVO(item), err
	}
	if item.Operation.ID == in.RunID {
		if item.Operation.RequestDigest != digest || item.Operation.Kind != "run" {
			return sessionVO(item), ce.ErrConflict
		}
		return sessionVO(item), nil
	}
	seen, err := s.repository.HasRun(ctx, scope(item), in.RunID)
	if err != nil || seen {
		if err == nil {
			err = ce.ErrConflict
		}
		return sessionVO(item), err
	}
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, in.RunID)
	if err != nil {
		return sessionVO(item), err
	}
	defer release()
	var turn uint64
	user := conversation.Message{Role: conversation.RoleUser, Payload: conversation.MessagePayload{Content: []conversation.ContentBlock{{Type: conversation.ContentTypeText, Text: in.Content}}}}
	for _, url := range in.ImageURLs {
		user.Payload.Content = append(user.Payload.Content, conversation.ContentBlock{Type: conversation.ContentTypeImage, Image: &conversation.ImageContent{URL: url}})
	}
	item, replay, err := s.admit(ctx, item, in.TrainingMutationDTO, "run", digest, func(tx repo.Tx, current training.Session) error {
		var e error
		turn, e = tx.BeginTurn(scope(current), in.RunID, user)
		return e
	})
	if err != nil || replay {
		return sessionVO(item), err
	}
	runCtx, complete := s.trackOperation(ctx, item, release)
	defer complete()
	history, runErr := s.repository.History(runCtx, scope(item), turn, 1000)
	var result pi.RunResult
	if runErr == nil {
		var listener pi.EventListener
		if len(listeners) > 0 {
			listener = listeners[0]
		}
		result, runErr = s.author.Run(runCtx, item, in, history, listener)
	}
	if runErr == nil {
		runErr = runCtx.Err()
	}
	final, finishCancel := finalizeContext()
	defer finishCancel()
	partial := runErr != nil
	checkpoint, checkpointErr := s.bundles.Checkpoint(final, candidate(item), bundle.CheckpointMetadata{RunID: in.RunID, ActorID: p.UserID, At: time.Now().UTC(), Partial: partial})
	if checkpointErr == nil {
		item.CandidateHead = checkpoint.Head
		item.CandidatePartial = partial
	} else {
		item.CandidatePartial = true
	}
	runErr = errors.Join(runErr, checkpointErr)
	messages, invocations := mapping.MapTrainingResult(result, in.RunID)
	err = s.repository.WithAgentTx(final, item.TenantID, item.AgentID, func(tx repo.Tx) error {
		current, err := tx.SessionForUpdate(scope(item))
		if err != nil {
			return err
		}
		if current.RowVersion != item.RowVersion || current.Operation.ID != in.RunID || !current.Running() {
			return ce.ErrConflict
		}
		if err = tx.FinishTurn(scope(item), in.RunID, turn, messages, invocations); err != nil {
			return err
		}
		now, err := tx.NowUTC()
		if err != nil {
			return err
		}
		item.Operation.FinishedAt = &now
		item.Operation.State = training.OperationCompleted
		if runErr != nil {
			item.Operation.State = training.OperationFailed
		}
		if item.IsExpired(now) {
			item.Operation.State = training.OperationInterrupted
			item.Status = training.Expired
			a, err := tx.AgentForUpdate()
			if err != nil {
				return err
			}
			if err = tx.SetTrainingPointer(item.ID, nil, a.RowVersion); err != nil {
				return err
			}
		}
		if err = tx.SaveSession(item, item.RowVersion); err != nil {
			return err
		}
		item.RowVersion++
		return nil
	})
	return sessionVO(item), errors.Join(runErr, err)
}
