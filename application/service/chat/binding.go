package chat

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/application/service/agentruntime"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/domain/repository"
	agentrepo "github.com/PycMono/go-reagent/domain/repository/agent"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type BoundRepository interface {
	CreateBound(context.Context, *conversationentity.Conversation) error
	CommitAgentVersion(context.Context, string, string, string, string) error
}
type Platform struct {
	Agents               agentrepo.Repository
	Bindings             BoundRepository
	History              conversationrepo.IConversationRepository
	Runtimes             *agentruntime.Manager
	HistoryLimit         int
	ProfileCompatibility bool
}

func NewBoundService(management conversationrepo.IConversationManagementRepository, history conversationrepo.IConversationRepository,
	ids repository.IIDService, agents agentrepo.Repository, runtimes *agentruntime.Manager, cfg *config.Config) (*Service, error) {
	bindings, ok := management.(BoundRepository)
	if !ok {
		return nil, errors.New("conversation repository has no Agent binding support")
	}
	if !cfg.Conversation.Enabled {
		return nil, errors.New("multi-Agent chat requires enabled MySQL conversation persistence")
	}
	return &Service{repository: management, ids: ids, active: make(map[string]*activeRunEntry), platform: &Platform{Agents: agents, Bindings: bindings, History: history, Runtimes: runtimes, HistoryLimit: cfg.Conversation.HistoryMessageLimit}}, nil
}
func (s *Service) requireOwner(ctx context.Context, userID string) error {
	if s == nil {
		return commonerrors.ErrInternal
	}
	if s.platform == nil {
		return nil
	}
	p, err := identity.Require(ctx)
	if err != nil {
		return err
	}
	if p.UserID != userID {
		return commonerrors.ErrUnauthorized
	}
	return nil
}
func (s *Service) scopedRunKey(ctx context.Context, userID, id string) string {
	if s.platform == nil {
		return activeRunKey(userID, id)
	}
	p, _ := identity.FromContext(ctx)
	return p.TenantID + "\x00" + activeRunKey(userID, id)
}
func (s *Service) createBound(ctx context.Context, userID string, in dto.CreateConversationDTO) (*vo.ConversationVO, error) {
	if err := s.requireOwner(ctx, userID); err != nil {
		return nil, err
	}
	p, _ := identity.FromContext(ctx)
	if (in.AgentID == "") == (in.ProfileCode == "") {
		return nil, commonerrors.ErrInvalidParam
	}
	id := in.AgentID
	if in.ProfileCode != "" {
		if !s.platform.ProfileCompatibility {
			return nil, commonerrors.ErrInvalidParam
		}
		seed, err := s.platform.Agents.FindBootstrap(ctx, p.TenantID, in.ProfileCode)
		if err != nil {
			return nil, err
		}
		id = seed.ID
	}
	a, err := s.platform.Agents.Find(ctx, p.TenantID, id)
	if err != nil {
		return nil, err
	}
	if a.Status != "enabled" || a.ActiveVersionID == nil {
		return nil, commonerrors.ErrConflict
	}
	c := &conversationentity.Conversation{ID: s.ids.NextID(), TenantID: p.TenantID, UserID: p.UserID, ConversationID: s.ids.NextID(), Name: UntitledChat,
		ProfileCode: a.TemplateCode, AgentID: a.ID, AgentVersionID: *a.ActiveVersionID, ConversationType: "chat", FollowLatest: true}
	if err := s.platform.Bindings.CreateBound(ctx, c); err != nil {
		return nil, err
	}
	result := conversationVO(c, 0)
	bindPresentation(result, c, a.Name, a.Presentation.Icon, a.Status)
	return result, nil
}
func bindPresentation(v *vo.ConversationVO, c *conversationentity.Conversation, name, icon, status string) {
	v.AgentID = c.AgentID
	v.AgentVersionID = c.AgentVersionID
	v.FollowLatest = c.FollowLatest
	v.AgentName = name
	v.AgentIcon = icon
	v.AgentStatus = status
}
func (s *Service) boundVO(ctx context.Context, c *conversationentity.Conversation, total int64) (*vo.ConversationVO, error) {
	p, err := identity.Require(ctx)
	if err != nil {
		return nil, err
	}
	if c == nil || c.TenantID != p.TenantID || c.UserID != p.UserID || c.ConversationType != "chat" || c.AgentID == "" {
		return nil, commonerrors.ErrNotFound
	}
	a, err := s.platform.Agents.Find(ctx, p.TenantID, c.AgentID)
	if err != nil {
		return nil, err
	}
	v := conversationVO(c, total)
	bindPresentation(v, c, a.Name, a.Presentation.Icon, a.Status)
	return v, nil
}
func (s *Service) GetConversation(ctx context.Context, userID, id string) (*vo.ConversationVO, error) {
	if err := s.requireOwner(ctx, userID); err != nil {
		return nil, err
	}
	c, found, err := s.repository.FindByUserIDAndConversationID(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, commonerrors.ErrNotFound
	}
	if s.platform != nil {
		return s.boundVO(ctx, c, 0)
	}
	return conversationVO(c, 0), nil
}
func (s *Service) listBound(ctx context.Context, userID string, q dto.ListConversationsQuery) (*vo.ConversationPageVO, error) {
	if err := s.requireOwner(ctx, userID); err != nil {
		return nil, err
	}
	if q.ProfileCode != "" {
		return nil, commonerrors.ErrInvalidParam
	}
	n, err := normalizeLimit(q.Limit)
	if err != nil {
		return nil, err
	}
	var cursor *conversationrepo.ListCursor
	if q.Cursor != "" {
		cursor, err = decodeConversationCursor(q.Cursor)
		if err != nil {
			return nil, commonerrors.ErrInvalidParam
		}
	}
	page, err := s.repository.ListByUserID(ctx, conversationrepo.ListQuery{UserID: userID, AgentID: q.AgentID, Keyword: strings.TrimSpace(q.Keyword), Cursor: cursor, Limit: n})
	if err != nil {
		return nil, err
	}
	result := &vo.ConversationPageVO{Items: []*vo.ConversationVO{}}
	for _, item := range page.Items {
		if item == nil || item.Conversation == nil {
			continue
		}
		v, err := s.boundVO(ctx, item.Conversation, item.MessageTotal)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, v)
	}
	if page.HasMore && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1].Conversation
		result.NextCursor = encodeConversationCursor(conversationrepo.ListCursor{ID: last.ID, UpdatedAt: last.UpdatedAt})
	}
	return result, nil
}
func (s *Service) stopBeforeDelete(ctx context.Context, userID, id string) error {
	key := s.scopedRunKey(ctx, userID, id)
	s.activeMu.Lock()
	entry := s.active[key]
	s.activeMu.Unlock()
	if entry == nil {
		return nil
	}
	entry.cancel()
	if entry.done != nil {
		select {
		case <-entry.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *Service) deleteBound(ctx context.Context, userID, id string) error {
	key := s.scopedRunKey(ctx, userID, id)
	s.activeMu.Lock()
	if s.deleting == nil {
		s.deleting = make(map[string]bool)
	}
	if s.deleting[key] {
		s.activeMu.Unlock()
		return commonerrors.ErrConflict
	}
	s.deleting[key] = true
	s.activeMu.Unlock()
	defer func() { s.activeMu.Lock(); delete(s.deleting, key); s.activeMu.Unlock() }()
	if err := s.stopBeforeDelete(ctx, userID, id); err != nil {
		return err
	}
	return s.repository.Delete(ctx, userID, id)
}
func (s *Service) startBoundRun(ctx context.Context, userID, id string, in dto.StartRunDTO) (*ActiveRun, error) {
	if err := s.requireOwner(ctx, userID); err != nil {
		return nil, err
	}
	content := strings.TrimSpace(in.Content)
	if content == "" || !utf8.ValidString(content) || !identity.ValidID(id) || len(in.ImageURLs) > pi.MaxImagesPerMessage {
		return nil, commonerrors.ErrInvalidParam
	}
	images := make([]string, 0, len(in.ImageURLs))
	for _, raw := range in.ImageURLs {
		url := strings.TrimSpace(raw)
		if err := ai.ImageBlock(url).Validate(); err != nil {
			return nil, commonerrors.ErrInvalidParam
		}
		images = append(images, url)
	}
	key := s.scopedRunKey(ctx, userID, id)
	s.activeMu.Lock()
	if s.deleting[key] {
		s.activeMu.Unlock()
		return nil, commonerrors.ErrConflict
	}
	if _, ok := s.active[key]; ok {
		s.activeMu.Unlock()
		return nil, commonerrors.ErrConflict
	}
	runID := s.ids.NextID()
	runCtx, cancel := context.WithCancel(ctx)
	s.active[key] = &activeRunEntry{id: runID, cancel: cancel, done: make(chan struct{})}
	s.activeMu.Unlock()
	ready := false
	defer func() {
		if !ready {
			s.releaseRun(key, runID)
		}
	}()
	p, _ := identity.FromContext(ctx)
	for attempt := 0; attempt < 2; attempt++ {
		c, found, err := s.repository.FindByUserIDAndConversationID(runCtx, userID, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, commonerrors.ErrNotFound
		}
		if c.TenantID != p.TenantID || c.UserID != p.UserID || c.ConversationType != "chat" {
			return nil, commonerrors.ErrNotFound
		}
		a, err := s.platform.Agents.Find(runCtx, p.TenantID, c.AgentID)
		if err != nil {
			return nil, err
		}
		if a.Status != "enabled" || a.ActiveVersionID == nil {
			return nil, commonerrors.ErrConflict
		}
		target := c.AgentVersionID
		if c.FollowLatest {
			target = *a.ActiveVersionID
		}
		version, err := s.platform.Agents.FindVersion(runCtx, p.TenantID, c.AgentID, target)
		if err != nil {
			return nil, err
		}
		snapshot, err := agentruntime.ParseVersion(version)
		if err != nil {
			return nil, err
		}
		lease, err := s.platform.Runtimes.Acquire(runCtx, agentruntime.Request{Key: agentruntime.Key{Kind: "chat", TenantID: p.TenantID, AgentID: c.AgentID, ConversationID: id, VersionID: target, SpecDigest: version.SpecDigest}, Version: version})
		if err != nil {
			if errors.Is(err, agentruntime.ErrBusy) {
				return nil, commonerrors.ErrBusy
			}
			return nil, err
		}
		if err := s.platform.Bindings.CommitAgentVersion(runCtx, userID, id, c.AgentVersionID, target); err != nil {
			lease.Release()
			if errors.Is(err, commonerrors.ErrConflict) {
				continue
			}
			return nil, err
		}
		runner := conversation.NewRunner(lease.Runner, s.platform.History, snapshot.Runtime.HistoryMessageLimit, snapshot.Runtime.Limits)
		events := make(chan vo.RunEventVO, runEventQueueSize)
		events <- vo.RunEventVO{Type: vo.RunEventRunStarted, RunID: runID}
		ready = true
		go func() {
			defer lease.Release()
			s.executeRun(runCtx, key, userID, id, runID, content, images, a.TemplateCode, nil, events, runner)
		}()
		return &ActiveRun{ID: runID, Events: events}, nil
	}
	return nil, commonerrors.ErrConflict
}
