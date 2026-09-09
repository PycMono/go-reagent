// Package identity carries verified application identity above the generic SDK.
package identity

import (
	"context"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	commonerrors "github.com/PycMono/go-reagent/common/errors"
)

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

type Principal struct {
	TenantID, UserID string
	Role             Role
}
type Authenticator interface {
	Authenticate(*http.Request) (Principal, error)
}
type contextKey struct{}

func ValidID(s string) bool {
	if s == "" || len(s) > 128 || !utf8.ValidString(s) || strings.TrimSpace(s) != s || s == "." || s == ".." || strings.ContainsAny(s, "/\\") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (p Principal) Validate() error {
	if !ValidID(p.TenantID) || !ValidID(p.UserID) || (p.Role != RoleUser && p.Role != RoleAdmin) {
		return commonerrors.ErrUnauthorized
	}
	return nil
}
func (p Principal) RequireAdmin() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Role != RoleAdmin {
		return commonerrors.ErrForbidden
	}
	return nil
}
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}
func FromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok && p.Validate() == nil
}
func Require(ctx context.Context) (Principal, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return Principal{}, commonerrors.ErrUnauthorized
	}
	return p, nil
}
