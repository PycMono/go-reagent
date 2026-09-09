package identity

import (
	"context"
	"errors"
	"testing"

	commonerrors "github.com/PycMono/go-reagent/common/errors"
)

func TestPrincipalAuthority(t *testing.T) {
	for _, tc := range []struct {
		p    Principal
		want error
	}{
		{Principal{"tenant", "user", RoleAdmin}, nil},
		{Principal{"tenant", "user", RoleUser}, commonerrors.ErrForbidden},
		{Principal{"", "user", RoleAdmin}, commonerrors.ErrUnauthorized},
		{Principal{"tenant", "", RoleAdmin}, commonerrors.ErrUnauthorized},
		{Principal{"tenant", "user", "owner"}, commonerrors.ErrUnauthorized},
		{Principal{"../tenant", "user", RoleAdmin}, commonerrors.ErrUnauthorized},
		{Principal{"tenant", "user\x00", RoleAdmin}, commonerrors.ErrUnauthorized},
	} {
		if got := tc.p.RequireAdmin(); !errors.Is(got, tc.want) {
			t.Fatalf("%+v got %v want %v", tc.p, got, tc.want)
		}
	}
}

func TestContextKeepsExactIdentity(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("untrusted context has authority")
	}
	p := Principal{"CustomerA", "UserA", RoleAdmin}
	got, ok := FromContext(WithPrincipal(context.Background(), p))
	if !ok || got != p {
		t.Fatalf("lost exact identity: %+v", got)
	}
	if _, ok := FromContext(WithPrincipal(context.Background(), Principal{})); ok {
		t.Fatal("invalid identity accepted")
	}
}
