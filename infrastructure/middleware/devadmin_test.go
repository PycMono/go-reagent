package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PycMono/go-context-sdk/bizctx"
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/gin-gonic/gin"
)

func newUpgradeRouter(t *testing.T, token string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 生产环境由 Visitor 中间件先注入访客 UserID；测试用等价桩。
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(bizctx.WithKV(c.Request.Context(), bizctx.UserID("visitor-1")))
		c.Next()
	})
	principal, err := AnonymousPrincipal("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	router.Use(principal)
	upgrade, err := DevAdminUpgrade(token)
	if err != nil {
		t.Fatal(err)
	}
	router.GET("/probe", upgrade, func(c *gin.Context) {
		p, ok := identity.FromContext(c.Request.Context())
		if !ok {
			c.String(http.StatusInternalServerError, "no principal")
			return
		}
		c.String(http.StatusOK, string(p.Role))
	})
	return router
}

func TestDevAdminUpgrade(t *testing.T) {
	const token = "local-dev-admin-0001"
	router := newUpgradeRouter(t, token)

	for _, tt := range []struct {
		name, header, cookie, want string
	}{
		{name: "header match", header: token, want: string(identity.RoleAdmin)},
		{name: "cookie match", cookie: token, want: string(identity.RoleAdmin)},
		{name: "no token stays user", want: string(identity.RoleUser)},
		{name: "wrong token stays user", header: "nope", want: string(identity.RoleUser)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/probe", nil)
			if tt.header != "" {
				req.Header.Set(devAdminTokenHeader, tt.header)
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: DevAdminCookieName, Value: tt.cookie})
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Body.String() != tt.want {
				t.Fatalf("role = %q, want %q", recorder.Body.String(), tt.want)
			}
		})
	}

	if _, err := DevAdminUpgrade(""); err == nil {
		t.Fatal("DevAdminUpgrade() with empty token = nil error, want error")
	}
}