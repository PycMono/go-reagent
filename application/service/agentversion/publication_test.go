package agentversion

import (
	"context"
	"errors"
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"testing"
)

func TestPublicationRequiresAdminAndHumanConfirmation(t *testing.T) {
	s := &PublicationService{}
	user := identity.Principal{TenantID: "t", UserID: "u", Role: identity.RoleUser}
	if _, err := s.ReleaseModel(context.Background(), user, "a", dto.ModelConfigReleaseDTO{}); !errors.Is(err, commonerrors.ErrForbidden) {
		t.Fatalf("user release: %v", err)
	}
	user.Role = identity.RoleAdmin
	if _, err := s.ReleaseModel(context.Background(), user, "a", dto.ModelConfigReleaseDTO{}); !errors.Is(err, commonerrors.ErrInvalidParam) {
		t.Fatalf("unconfirmed release: %v", err)
	}
}
