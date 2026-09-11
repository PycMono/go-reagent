package agenttraining

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/PycMono/go-reagent/application/identity"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	ce "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
)

func (s *Service) Restore(ctx context.Context, p identity.Principal, id, checkpoint string, in dto.TrainingMutationDTO) (vo.TrainingSessionVO, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return vo.TrainingSessionVO{}, err
	}
	digest, err := requestDigest("restore", struct {
		Mutation   dto.TrainingMutationDTO
		Checkpoint string
	}{in, checkpoint})
	if err != nil {
		return sessionVO(item), err
	}
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, in.RequestID)
	if err != nil {
		return sessionVO(item), err
	}
	defer release()
	item, replay, err := s.admit(ctx, item, in, "restore", digest)
	if err != nil || replay {
		return sessionVO(item), err
	}
	cp, opErr := s.bundles.Restore(ctx, candidate(item), checkpoint)
	if errors.Is(opErr, bundle.ErrRecoveryRequired) {
		return sessionVO(item), opErr
	}
	if opErr == nil {
		item.CandidateHead = cp.Head
		item.CandidatePartial = cp.Metadata.Partial
	}
	final, cancel := finalizeContext()
	defer cancel()
	item, err = s.finish(final, item, opErr)
	return sessionVO(item), errors.Join(opErr, err)
}
func (s *Service) PatchConfig(ctx context.Context, p identity.Principal, id string, in dto.TrainingConfigDTO) (vo.TrainingSessionVO, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return vo.TrainingSessionVO{}, err
	}
	if len(in.ModelConfig.Parameters) > 0 {
		var params map[string]json.RawMessage
		if agentversion.DecodeStrict(in.ModelConfig.Parameters, &params) != nil || len(params) > 0 {
			return sessionVO(item), ce.ErrInvalidParam
		}
	}
	selected, err := s.capture.CaptureAgentSnapshot(in.ModelConfig.ProviderRef, in.ModelConfig.ModelID, []string{})
	if err != nil {
		return sessionVO(item), err
	}
	snapshot, err := agentversion.ParseSnapshot(item.CandidateConfig)
	if err != nil {
		return sessionVO(item), err
	}
	snapshot.Model = selected.Model
	config, err := json.Marshal(snapshot)
	if err != nil {
		return sessionVO(item), err
	}
	if _, err = agentversion.ParseSnapshot(config); err != nil {
		return sessionVO(item), err
	}
	digest, err := requestDigest("config", in)
	if err != nil {
		return sessionVO(item), err
	}
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, in.RequestID)
	if err != nil {
		return sessionVO(item), err
	}
	defer release()
	item, replay, err := s.admit(ctx, item, in.TrainingMutationDTO, "config", digest)
	if err != nil || replay {
		return sessionVO(item), err
	}
	item.CandidateConfig = config
	final, cancel := finalizeContext()
	defer cancel()
	item, err = s.finish(final, item, nil)
	return sessionVO(item), err
}
