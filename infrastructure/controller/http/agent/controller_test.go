package agent

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/gin-gonic/gin"
)

func TestManagementRequestRejectsUnknownAndDuplicateClaims(t *testing.T) {
	for _, body := range []string{`{"name":"A","tenant_id":"other"}`, `{"name":"A","name":"B"}`} {
		r := gin.New()
		r.POST("/", func(c *gin.Context) {
			var in struct {
				Name string `json:"name"`
			}
			if err := decode(c, &in); err == nil {
				t.Fatal("ambiguous input accepted")
			}
			c.Status(400)
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
func TestManagementGuardRejectsUserAndCookieWithoutOrigin(t *testing.T) {
	for _, tc := range []struct {
		role           identity.Role
		cookie, origin string
		status         int
	}{{identity.RoleUser, "", "", 403}, {identity.RoleAdmin, "sid=test", "", 403}, {identity.RoleAdmin, "sid=test", "https://other.invalid", 403}, {identity.RoleAdmin, "sid=test", "http://example.com", 200}} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(identity.WithPrincipal(context.Background(), identity.Principal{TenantID: "t", UserID: "u", Role: tc.role}))
		})
		r.POST("/", managementGuard(), func(c *gin.Context) { c.Status(200) })
		req := httptest.NewRequest("POST", "http://example.com/", nil)
		req.Header.Set("Cookie", tc.cookie)
		req.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%+v got %d", tc, w.Code)
		}
	}
}
