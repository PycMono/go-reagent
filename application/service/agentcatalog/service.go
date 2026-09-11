package agentcatalog

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	agentrepo "github.com/PycMono/go-reagent/domain/repository/agent"
	profilerepo "github.com/PycMono/go-reagent/domain/repository/agentprofile"
)

type IDSource interface{ NextID() string }
type InitialBuilder interface {
	PrepareInitial(context.Context, identity.Principal, agent.Agent, string, dto.ModelChoice) (agent.Version, error)
}
type Service struct {
	repository agentrepo.Repository
	builder    InitialBuilder
	templates  profilerepo.Catalog
	ids        IDSource
	cursorKey  []byte
}

func NewService(repository agentrepo.Repository, builder InitialBuilder, templates profilerepo.Catalog, ids IDSource, cursorKey []byte) (*Service, error) {
	if len(cursorKey) < 32 {
		return nil, errors.New("catalog cursor key must be at least 32 bytes")
	}
	return &Service{repository: repository, builder: builder, templates: templates, ids: ids, cursorKey: append([]byte(nil), cursorKey...)}, nil
}

type cursor struct {
	Tenant, Keyword, Agent, Position string
	Admin                            bool
}

func (s *Service) encodeCursor(c cursor) string {
	raw, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) decodeCursor(raw string, scope cursor) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) > 4096 {
		return "", commonerrors.ErrInvalidParam
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return "", commonerrors.ErrInvalidParam
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", commonerrors.ErrInvalidParam
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", commonerrors.ErrInvalidParam
	}
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write(data)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", commonerrors.ErrInvalidParam
	}
	var c cursor
	if json.Unmarshal(data, &c) != nil || c.Tenant != scope.Tenant || c.Keyword != scope.Keyword || c.Admin != scope.Admin || c.Agent != scope.Agent {
		return "", commonerrors.ErrInvalidParam
	}
	return c.Position, nil
}
func limit(n int) (int, error) {
	if n == 0 {
		return 20, nil
	}
	if n < 1 || n > 100 {
		return 0, commonerrors.ErrInvalidParam
	}
	return n, nil
}
func (s *Service) List(ctx context.Context, p identity.Principal, q dto.ListAgentsQuery) (*vo.AgentPageVO, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if q.IncludeArchived {
		if err := p.RequireAdmin(); err != nil {
			return nil, err
		}
	}
	n, err := limit(q.Limit)
	if err != nil {
		return nil, err
	}
	q.Keyword = strings.TrimSpace(q.Keyword)
	if len(q.Keyword) > 1024 {
		return nil, commonerrors.ErrInvalidParam
	}
	scope := cursor{Tenant: p.TenantID, Keyword: q.Keyword, Admin: q.IncludeArchived}
	position, err := s.decodeCursor(q.Cursor, scope)
	if err != nil {
		return nil, err
	}
	page, err := s.repository.List(ctx, agentrepo.ListQuery{TenantID: p.TenantID, Keyword: q.Keyword, Cursor: position, Limit: n, IncludeArchived: q.IncludeArchived})
	if err != nil {
		return nil, err
	}
	result := &vo.AgentPageVO{Items: make([]*vo.AgentVO, 0, len(page.Items)), DefaultAgentID: page.DefaultAgentID}
	for _, a := range page.Items {
		result.Items = append(result.Items, Project(a, p.Role == identity.RoleAdmin))
	}
	if page.NextCursor != "" {
		scope.Position = page.NextCursor
		result.NextCursor = s.encodeCursor(scope)
	}
	return result, nil
}
func (s *Service) Get(ctx context.Context, p identity.Principal, id string) (*vo.AgentVO, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	a, err := s.repository.Find(ctx, p.TenantID, id)
	if err != nil {
		return nil, err
	}
	if p.Role != identity.RoleAdmin && (a.Status != "enabled" || a.ActiveVersionID == nil) {
		return nil, commonerrors.ErrNotFound
	}
	return Project(a, p.Role == identity.RoleAdmin), nil
}
func Project(a agent.Agent, admin bool) *vo.AgentVO {
	v := &vo.AgentVO{ID: a.ID, Name: a.Name, Description: a.Description, Icon: a.Presentation.Icon, Welcome: a.Presentation.Welcome, Starters: append([]agent.Starter{}, a.Presentation.Starters...), Presentation: a.Presentation, Status: a.Status, Selectable: a.Status == "enabled" && a.ActiveVersionID != nil, ActiveVersionID: a.ActiveVersionID, TemplateCode: a.TemplateCode, RowVersion: a.RowVersion}
	if admin {
		v.ActiveTrainingSessionID = a.ActiveTrainingSessionID
	}
	return v
}
func (s *Service) Templates(_ context.Context, p identity.Principal) ([]vo.AgentTemplateVO, error) {
	if err := p.RequireAdmin(); err != nil {
		return nil, err
	}
	result := []vo.AgentTemplateVO{}
	for _, t := range s.templates.List() {
		if t.Selectable {
			result = append(result, vo.AgentTemplateVO{Code: t.Code, Name: t.Name, Description: t.Description, Icon: t.Icon})
		}
	}
	return result, nil
}
func validatePresentation(a agent.Agent) error {
	if a.Name == "" || utf8.RuneCountInString(a.Name) > 128 || !utf8.ValidString(a.Name) || !utf8.ValidString(a.Description) ||
		!utf8.ValidString(a.Presentation.Welcome) || len(a.Description) > 8192 || len(a.Presentation.Welcome) > 8192 || len(a.Presentation.Starters) > 8 {
		return commonerrors.ErrInvalidParam
	}
	switch a.Presentation.Icon {
	case "", "sparkles", "general", "writing", "learning", "workplace", "health", "parenting", "automotive", "legal", "message-circle", "book-open", "pen-tool", "briefcase", "heart", "car", "scale", "graduation-cap", "pen-line", "heart-pulse", "car-front", "baby":
	default:
		return commonerrors.ErrInvalidParam
	}
	for _, starter := range a.Presentation.Starters {
		if !utf8.ValidString(starter.Title) || !utf8.ValidString(starter.Prompt) || len(starter.Title) > 256 || len(starter.Prompt) > 4096 || strings.TrimSpace(starter.Title) == "" || strings.TrimSpace(starter.Prompt) == "" {
			return commonerrors.ErrInvalidParam
		}
	}
	if a.Status != "enabled" && a.Status != "archived" {
		return commonerrors.ErrInvalidParam
	}
	return nil
}
func (s *Service) Create(ctx context.Context, p identity.Principal, in dto.CreateAgentDTO) (*vo.AgentVO, error) {
	return s.create(ctx, p, in, nil)
}
func (s *Service) create(ctx context.Context, p identity.Principal, in dto.CreateAgentDTO, bootstrap *string) (*vo.AgentVO, error) {
	if err := p.RequireAdmin(); err != nil {
		return nil, err
	}
	if s.builder == nil || s.ids == nil || s.templates == nil {
		return nil, commonerrors.ErrInternal
	}
	if in.TemplateCode == "" {
		in.TemplateCode = "general"
	}
	template, ok := s.templates.Find(in.TemplateCode)
	if !ok || !template.Selectable {
		return nil, commonerrors.ErrInvalidParam
	}
	if len(in.ModelConfig.Parameters) > 0 {
		if !validEmptyParameters(in.ModelConfig.Parameters) {
			return nil, commonerrors.ErrInvalidParam
		}
	}
	a := agent.Agent{ID: s.ids.NextID(), TenantID: p.TenantID, Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description), Status: "enabled", Presentation: in.Presentation, TemplateCode: in.TemplateCode, BootstrapKey: bootstrap, CreatedBy: p.UserID}
	if a.Presentation.Icon == "" {
		a.Presentation.Icon = template.Icon
	}
	if a.Presentation.Welcome == "" {
		a.Presentation.Welcome = template.Welcome
	}
	if a.Presentation.Starters == nil {
		for _, starter := range template.Starters {
			a.Presentation.Starters = append(a.Presentation.Starters, agent.Starter{Title: starter.Title, Prompt: starter.Prompt})
		}
	}
	if err := validatePresentation(a); err != nil {
		return nil, err
	}
	a, err := s.repository.ReserveDraft(ctx, a)
	if err != nil {
		return nil, err
	}
	return s.completeDraft(ctx, p, a, in.ModelConfig)
}

func (s *Service) completeDraft(ctx context.Context, p identity.Principal, a agent.Agent, model dto.ModelChoice) (*vo.AgentVO, error) {
	if a.Status != "enabled" || a.ActiveVersionID != nil {
		return Project(a, true), commonerrors.ErrConflict
	}
	v, err := s.builder.PrepareInitial(ctx, p, a, s.ids.NextID(), model)
	if err != nil {
		return Project(a, true), err
	}
	if err := s.repository.CommitInitial(ctx, a, v, a.RowVersion); err != nil {
		return Project(a, true), err
	}
	if completion, ok := s.builder.(interface {
		CompleteInitial(context.Context, agent.Version) error
	}); ok {
		// The database is authoritative once committed. A failed cleanup keeps
		// the journal for recovery but does not turn a successful creation into failure.
		_ = completion.CompleteInitial(ctx, v)
	}
	a, err = s.repository.Find(ctx, p.TenantID, a.ID)
	return Project(a, true), err
}

func validEmptyParameters(raw json.RawMessage) bool {
	var parameters map[string]json.RawMessage
	return json.Unmarshal(raw, &parameters) == nil && parameters != nil && len(parameters) == 0
}
func (s *Service) Patch(ctx context.Context, p identity.Principal, id string, in dto.PatchAgentDTO) (*vo.AgentVO, error) {
	if err := p.RequireAdmin(); err != nil {
		return nil, err
	}
	if in.Name == nil && in.Description == nil && in.Status == nil && in.Presentation == nil {
		return nil, commonerrors.ErrInvalidParam
	}
	a, err := s.repository.Find(ctx, p.TenantID, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		a.Description = strings.TrimSpace(*in.Description)
	}
	if in.Status != nil {
		a.Status = *in.Status
	}
	if in.Presentation != nil {
		a.Presentation = *in.Presentation
	}
	if err := validatePresentation(a); err != nil {
		return nil, err
	}
	if err := s.repository.UpdatePresentation(ctx, a, in.ExpectedRowVersion); err != nil {
		return nil, err
	}
	a, err = s.repository.Find(ctx, p.TenantID, id)
	return Project(a, true), err
}
func (s *Service) Versions(ctx context.Context, p identity.Principal, id, rawCursor string, n int) (*vo.AgentVersionPageVO, error) {
	if err := p.RequireAdmin(); err != nil {
		return nil, err
	}
	n, err := limit(n)
	if err != nil {
		return nil, err
	}
	a, err := s.repository.Find(ctx, p.TenantID, id)
	if err != nil {
		return nil, err
	}
	scope := cursor{Tenant: p.TenantID, Agent: id, Admin: true}
	pos, err := s.decodeCursor(rawCursor, scope)
	if err != nil {
		return nil, err
	}
	var before uint64
	if pos != "" {
		before, err = strconv.ParseUint(pos, 10, 64)
		if err != nil || before == 0 {
			return nil, commonerrors.ErrInvalidParam
		}
	}
	versions, err := s.repository.ListVersions(ctx, p.TenantID, id, before, n)
	if err != nil {
		return nil, err
	}
	page := &vo.AgentVersionPageVO{Items: []vo.AgentVersionVO{}}
	for _, v := range versions.Items {
		page.Items = append(page.Items, vo.AgentVersionVO{ID: v.ID, Number: v.Number, PublishedAt: v.PublishedAt, PublishedBy: v.PublishedBy, ChangeSummary: v.ChangeSummary, SourceTrainingSessionID: v.SourceTrainingSessionID, Active: a.ActiveVersionID != nil && *a.ActiveVersionID == v.ID})
	}
	if versions.HasMore && len(versions.Items) > 0 {
		scope.Position = strconv.FormatUint(versions.Items[len(versions.Items)-1].Number, 10)
		page.NextCursor = s.encodeCursor(scope)
	}
	return page, nil
}
func (s *Service) Bootstrap(ctx context.Context, p identity.Principal, code string) (*vo.AgentVO, error) {
	if err := p.RequireAdmin(); err != nil {
		return nil, err
	}
	existing, err := s.repository.FindBootstrap(ctx, p.TenantID, code)
	if err == nil {
		if existing.ActiveVersionID == nil {
			return s.completeDraft(ctx, p, existing, dto.ModelChoice{})
		}
		return Project(existing, true), nil
	}
	if !errors.Is(err, commonerrors.ErrNotFound) {
		return nil, err
	}
	template, ok := s.templates.Find(code)
	if !ok {
		return nil, commonerrors.ErrInvalidParam
	}
	created, err := s.create(ctx, p, dto.CreateAgentDTO{Name: template.Name, Description: template.Description, TemplateCode: code}, &code)
	if err != nil {
		if a, lookupErr := s.repository.FindBootstrap(ctx, p.TenantID, code); lookupErr == nil && a.ActiveVersionID != nil {
			return Project(a, true), nil
		}
	}
	return created, err
}
