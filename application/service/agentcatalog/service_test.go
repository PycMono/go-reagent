package agentcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	profile "github.com/PycMono/go-reagent/domain/entity/agentprofile"
	repo "github.com/PycMono/go-reagent/domain/repository/agent"
)

type catalogRepo struct {
	repo.Repository
	query    repo.ListQuery
	record   agent.Agent
	versions repo.VersionPage
}

func (r *catalogRepo) ListVersions(context.Context, string, string, uint64, int) (repo.VersionPage, error) {
	return r.versions, nil
}

func (r *catalogRepo) List(_ context.Context, q repo.ListQuery) (repo.ListPage, error) {
	r.query = q
	return repo.ListPage{Items: []agent.Agent{}, NextCursor: "1:a"}, nil
}
func (r *catalogRepo) Find(_ context.Context, tenant, id string) (agent.Agent, error) {
	if tenant != r.record.TenantID || id != r.record.ID {
		return agent.Agent{}, commonerrors.ErrNotFound
	}
	return r.record, nil
}
func TestCatalogScopesAndBindsCursor(t *testing.T) {
	r := &catalogRepo{}
	key := []byte("01234567890123456789012345678901")
	s, err := NewService(r, nil, nil, nil, key)
	if err != nil {
		t.Fatal(err)
	}
	p := identity.Principal{TenantID: "t", UserID: "u", Role: identity.RoleUser}
	page, err := s.List(context.Background(), p, dto.ListAgentsQuery{Keyword: "search"})
	if err != nil {
		t.Fatal(err)
	}
	if r.query.TenantID != "t" || r.query.Limit != 20 || r.query.IncludeArchived {
		t.Fatalf("wrong scope %+v", r.query)
	}
	if _, err := s.List(context.Background(), p, dto.ListAgentsQuery{Cursor: page.NextCursor, Keyword: "different"}); !errors.Is(err, commonerrors.ErrInvalidParam) {
		t.Fatalf("cursor crossed search: %v", err)
	}
	if _, err := s.List(context.Background(), p, dto.ListAgentsQuery{IncludeArchived: true}); !errors.Is(err, commonerrors.ErrForbidden) {
		t.Fatalf("admin catalog exposed: %v", err)
	}
	s2, _ := NewService(r, nil, nil, nil, key)
	if _, err := s2.List(context.Background(), p, dto.ListAgentsQuery{Cursor: page.NextCursor, Keyword: "search"}); err != nil {
		t.Fatalf("persistent cursor signer rejected cursor: %v", err)
	}
}
func TestUnpublishedAgentOnlyVisibleToAdmin(t *testing.T) {
	r := &catalogRepo{record: agent.Agent{ID: "a", TenantID: "t", Status: "enabled"}}
	s, _ := NewService(r, nil, nil, nil, []byte("01234567890123456789012345678901"))
	p := identity.Principal{TenantID: "t", UserID: "u", Role: identity.RoleUser}
	if _, err := s.Get(context.Background(), p, "a"); !errors.Is(err, commonerrors.ErrNotFound) {
		t.Fatal("draft exposed")
	}
	p.Role = identity.RoleAdmin
	v, err := s.Get(context.Background(), p, "a")
	if err != nil || v.Selectable {
		t.Fatalf("admin draft %v %+v", err, v)
	}
}

func TestNewServiceRejectsShortCursorKey(t *testing.T) {
	if _, err := NewService(&catalogRepo{}, nil, nil, nil, []byte("short")); err == nil {
		t.Fatal("short cursor key accepted")
	}
}

func TestPatchRejectsEmptyRequest(t *testing.T) {
	r := &catalogRepo{record: agent.Agent{ID: "a", TenantID: "t", Name: "A", Status: "enabled"}}
	s, _ := NewService(r, nil, nil, nil, []byte("01234567890123456789012345678901"))
	p := identity.Principal{TenantID: "t", UserID: "u", Role: identity.RoleAdmin}
	if _, err := s.Patch(context.Background(), p, "a", dto.PatchAgentDTO{}); !errors.Is(err, commonerrors.ErrInvalidParam) {
		t.Fatalf("empty patch: %v", err)
	}
}

func TestPresentationRejectsInvalidUTF8(t *testing.T) {
	a := agent.Agent{Name: "A", Description: string([]byte{0xff}), Status: "enabled"}
	if !errors.Is(validatePresentation(a), commonerrors.ErrInvalidParam) {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func TestPresentationAcceptsConfiguredTemplateIcons(t *testing.T) {
	for _, icon := range []string{"message-circle", "pen-line", "graduation-cap", "heart-pulse", "scale", "car-front", "briefcase", "baby"} {
		a := agent.Agent{Name: "A", Status: "enabled", Presentation: agent.Presentation{Icon: icon}}
		if err := validatePresentation(a); err != nil {
			t.Fatalf("configured icon %q rejected: %v", icon, err)
		}
	}
}

func TestModelParametersAllowOnlyWhitespaceEmptyObject(t *testing.T) {
	for _, raw := range []string{"{}", " { }\n"} {
		if !validEmptyParameters(json.RawMessage(raw)) {
			t.Fatalf("empty parameters %q rejected", raw)
		}
	}
	for _, raw := range []string{"null", `{"temperature":0}`, "[]", "{} trailing"} {
		if validEmptyParameters(json.RawMessage(raw)) {
			t.Fatalf("unsupported parameters %q accepted", raw)
		}
	}
}

func TestVersionsCursorUsesHasMore(t *testing.T) {
	r := &catalogRepo{record: agent.Agent{ID: "a", TenantID: "t", Status: "enabled"}, versions: repo.VersionPage{Items: []agent.Version{{ID: "v2", Number: 2}}, HasMore: false}}
	s, _ := NewService(r, nil, nil, nil, []byte("01234567890123456789012345678901"))
	p := identity.Principal{TenantID: "t", UserID: "u", Role: identity.RoleAdmin}
	page, err := s.Versions(context.Background(), p, "a", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != "" {
		t.Fatal("terminal exact-size page advertised a cursor")
	}
	r.versions.HasMore = true
	page, err = s.Versions(context.Background(), p, "a", "", 1)
	if err != nil || page.NextCursor == "" {
		t.Fatalf("non-terminal page cursor = %q err=%v", page.NextCursor, err)
	}
}

type completingBuilder struct{ completed bool }

func (b *completingBuilder) PrepareInitial(_ context.Context, _ identity.Principal, a agent.Agent, id string, _ dto.ModelChoice) (agent.Version, error) {
	return agent.Version{ID: id, TenantID: a.TenantID, AgentID: a.ID}, nil
}
func (b *completingBuilder) CompleteInitial(context.Context, agent.Version) error {
	b.completed = true
	return nil
}

type creationRepo struct {
	catalogRepo
	fail bool
}

func (r *creationRepo) FindBootstrap(_ context.Context, tenant, code string) (agent.Agent, error) {
	if r.record.TenantID != tenant || r.record.BootstrapKey == nil || *r.record.BootstrapKey != code {
		return agent.Agent{}, commonerrors.ErrNotFound
	}
	return r.record, nil
}

func TestBootstrapResumesExistingDraft(t *testing.T) {
	key := "general"
	r := &creationRepo{catalogRepo: catalogRepo{record: agent.Agent{ID: "existing", TenantID: "t", Status: "enabled", TemplateCode: "general", BootstrapKey: &key}}}
	b := &completingBuilder{}
	s, _ := NewService(r, b, creationTemplates{}, creationIDs{}, []byte("01234567890123456789012345678901"))
	p := identity.Principal{TenantID: "t", UserID: "admin", Role: identity.RoleAdmin}
	first, err := s.Bootstrap(context.Background(), p, key)
	if err != nil || first.ID != "existing" || first.ActiveVersionID == nil {
		t.Fatalf("draft not recovered: %+v %v", first, err)
	}
	second, err := s.Bootstrap(context.Background(), p, key)
	if err != nil || second.ID != first.ID || *second.ActiveVersionID != *first.ActiveVersionID {
		t.Fatal("bootstrap created duplicate", err)
	}
}

func (r *creationRepo) ReserveDraft(_ context.Context, a agent.Agent) (agent.Agent, error) {
	r.record = a
	return a, nil
}
func (r *creationRepo) CommitInitial(_ context.Context, a agent.Agent, v agent.Version, _ uint64) error {
	if r.fail {
		return errors.New("commit failed")
	}
	r.record.ActiveVersionID = &v.ID
	return nil
}

type creationIDs struct{}

func (creationIDs) NextID() string { return "id" }

type creationTemplates struct{}

func (creationTemplates) List() []profile.Profile { return nil }
func (creationTemplates) DefaultCode() string     { return "general" }
func (creationTemplates) Find(string) (profile.Profile, bool) {
	return profile.Profile{Code: "general", Selectable: true}, true
}
func TestCreationCompletesJournalOnlyAfterCommit(t *testing.T) {
	for _, fail := range []bool{true, false} {
		r := &creationRepo{fail: fail}
		b := &completingBuilder{}
		s, _ := NewService(r, b, creationTemplates{}, creationIDs{}, []byte("01234567890123456789012345678901"))
		_, err := s.Create(context.Background(), identity.Principal{TenantID: "t", UserID: "u", Role: identity.RoleAdmin}, dto.CreateAgentDTO{Name: "A"})
		if (err != nil) != fail || b.completed == fail {
			t.Fatalf("fail=%v completed=%v err=%v", fail, b.completed, err)
		}
	}
}
