package page

import (
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminPagesRequireAdministrator(t *testing.T) {
	renderer, err := NewRenderer(frontendTemplateRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	ctl := NewController(renderer)
	for _, role := range []identity.Role{"", identity.RoleUser, identity.RoleAdmin} {
		router := gin.New()
		if role != "" {
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(identity.WithPrincipal(c.Request.Context(), identity.Principal{TenantID: "tenant", UserID: "user", Role: role}))
			})
		}
		RegisterAdminPageRoutes(router, ctl)
		for _, path := range []string{"/admin/agents", "/admin/agents/new", "/admin/agents/a1", "/admin/training-sessions/t1"} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			expected := http.StatusOK
			if role == "" {
				expected = http.StatusUnauthorized
			} else if role == identity.RoleUser {
				expected = http.StatusForbidden
			}
			if response.Code != expected {
				t.Fatalf("role=%s path=%s status=%d body=%s", role, path, response.Code, response.Body.String())
			}
			if role == identity.RoleAdmin && !strings.Contains(response.Body.String(), "/static/js/pages/") {
				t.Fatal("missing admin application")
			}
		}
	}
}
