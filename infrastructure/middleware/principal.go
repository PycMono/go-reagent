package middleware

import (
	"errors"
	"net/http"

	"github.com/PycMono/go-context-sdk/bizctx"
	"github.com/PycMono/go-reagent/application/identity"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/gin-gonic/gin"
)

// HostPrincipal accepts only an embedding host's credential verifier. Headers
// carrying unverified role or tenant claims have no meaning here.
func HostPrincipal(auth identity.Authenticator) (gin.HandlerFunc, error) {
	if auth == nil {
		return nil, errors.New("identity host mode requires an Authenticator")
	}
	return func(c *gin.Context) {
		p, err := auth.Authenticate(c.Request)
		if errors.Is(err, commonerrors.ErrForbidden) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code": commonerrors.ErrForbidden.Code(), "msg": commonerrors.ErrForbidden.Message(), "data": nil,
			})
			return
		}
		if err != nil || p.Validate() != nil {
			rejectPrincipal(c)
			return
		}
		attachPrincipal(c, p)
		c.Next()
	}, nil
}

// AnonymousPrincipal must run after Visitor, and can only grant ordinary-user
// access to the deployment's explicitly configured tenant.
func AnonymousPrincipal(tenant string) (gin.HandlerFunc, error) {
	if !identity.ValidID(tenant) {
		return nil, errors.New("anonymous identity requires an explicit valid tenant")
	}
	return func(c *gin.Context) {
		p := identity.Principal{TenantID: tenant, UserID: bizctx.GetUserID(c.Request.Context()), Role: identity.RoleUser}
		if p.Validate() != nil {
			rejectPrincipal(c)
			return
		}
		attachPrincipal(c, p)
		c.Next()
	}, nil
}
func attachPrincipal(c *gin.Context, p identity.Principal) {
	ctx := identity.WithPrincipal(c.Request.Context(), p)
	ctx = bizctx.WithKV(ctx, bizctx.UserID(p.UserID))
	c.Request = c.Request.WithContext(ctx)
}
func rejectPrincipal(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "unauthorized", "data": nil})
}
