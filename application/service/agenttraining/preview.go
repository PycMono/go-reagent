package agenttraining

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/application/identity"
	bundle "github.com/PycMono/go-reagent/application/port/agentbundle"
	"github.com/PycMono/go-reagent/common/dto"
	ce "github.com/PycMono/go-reagent/common/errors"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

// Preview is a read-only trial of the current candidate. Trial history is local
// to the workbench and never appended to the author's training conversation.
func (s *Service) Preview(ctx context.Context, p identity.Principal, id string, in dto.TrainingPreviewDTO) (string, error) {
	item, err := s.owned(ctx, p, id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Content) == "" || !utf8.ValidString(in.Content) || len(in.Content) > 32000 || len(in.History) > 40 {
		return "", ce.ErrInvalidParam
	}
	total := len(in.Content)
	for _, m := range in.History {
		total += len(m.Content)
		if (m.Role != "user" && m.Role != "assistant") || !utf8.ValidString(m.Content) || strings.TrimSpace(m.Content) == "" {
			return "", ce.ErrInvalidParam
		}
	}
	if total > 128000 {
		return "", ce.ErrInvalidParam
	}
	if item.Terminal() || item.Running() || item.CandidatePartial {
		return "", ce.ErrConflict
	}
	author, ok := s.author.(interface {
		Preview(context.Context, training.Session, dto.TrainingPreviewDTO, string, string) (pi.RunResult, error)
	})
	if !ok {
		return "", errors.New("candidate preview unavailable")
	}
	op := s.ids.NextID()
	release, err := s.admission.Reserve(ctx, item.TenantID, item.AgentID, op)
	if err != nil {
		return "", err
	}
	defer release()
	current, err := s.repository.Find(ctx, scope(item))
	if err != nil {
		return "", err
	}
	if current.RowVersion != item.RowVersion || current.Running() {
		return "", ce.ErrConflict
	}
	ctx, cancel := context.WithDeadline(ctx, item.ExpiresAt)
	defer cancel()
	ref, err := s.bundles.FreezeCandidate(ctx, candidate(item), op)
	if err != nil {
		return "", err
	}
	defer func() {
		final, cancel := finalizeContext()
		defer cancel()
		_ = s.bundles.CleanupPreparation(final, bundle.PreparationArtifacts{TenantID: item.TenantID, AgentID: item.AgentID, VersionID: op, Ref: ref})
	}()
	root, err := s.bundles.MaterializeValidation(ctx, item.TenantID, item.AgentID, op, ref)
	if err != nil {
		return "", err
	}
	result, err := author.Preview(ctx, item, in, op, root)
	if err != nil {
		return "", err
	}
	var texts []string
	for _, message := range result.NewMessages {
		if message.Role == ai.RoleAssistant {
			content, err := message.Content.Text()
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(content) != "" {
				texts = append(texts, content)
			}
		}
	}
	return strings.Join(texts, "\n\n"), nil
}
