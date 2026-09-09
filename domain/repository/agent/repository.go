package agent

import (
	"context"

	agententity "github.com/PycMono/go-reagent/domain/entity/agent"
)

type ListQuery struct {
	TenantID        string
	Keyword         string
	Cursor          string
	Limit           int
	IncludeArchived bool
}

type ListPage struct {
	Items          []agententity.Agent
	NextCursor     string
	DefaultAgentID *string
}

type Repository interface {
	Find(context.Context, string, string) (agententity.Agent, error)
	List(context.Context, ListQuery) (ListPage, error)
	FindVersion(context.Context, string, string, string) (agententity.Version, error)
	ListVersions(context.Context, string, string, uint64, int) ([]agententity.Version, error)
	FindBootstrap(context.Context, string, string) (agententity.Agent, error)
	ReserveDraft(context.Context, agententity.Agent) (agententity.Agent, error)
	CommitInitial(context.Context, agententity.Agent, agententity.Version, uint64) error
	UpdatePresentation(context.Context, agententity.Agent, uint64) error
}
