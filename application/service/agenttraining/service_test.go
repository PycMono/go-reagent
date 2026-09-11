package agenttraining

import (
	"context"
	"errors"
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"testing"
)

func TestUnauthorizedTrainingNeverTouchesStorage(t *testing.T) {
	s := &Service{}
	for _, p := range []identity.Principal{{}, {TenantID: "t", UserID: "u", Role: identity.RoleUser}} {
		if _, err := s.Create(context.Background(), p, "a", dto.CreateTrainingDTO{}); !errors.Is(err, commonerrors.ErrForbidden) && !errors.Is(err, commonerrors.ErrUnauthorized) {
			t.Fatalf("create: %v", err)
		}
		if _, err := s.Get(context.Background(), p, "x"); !errors.Is(err, commonerrors.ErrForbidden) && !errors.Is(err, commonerrors.ErrUnauthorized) {
			t.Fatalf("get: %v", err)
		}
	}
}
