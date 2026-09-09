package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PycMono/go-context-sdk/bizctx"
	"github.com/PycMono/go-reagent/application/identity"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/gin-gonic/gin"
)

type testHostAuth func(*http.Request) (identity.Principal, error)

func (f testHostAuth) Authenticate(r *http.Request) (identity.Principal, error) { return f(r) }

func TestHostPrincipalIgnoresClaimsAndFailsClosed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		auth := testHostAuth(func(*http.Request) (identity.Principal, error) {
			if fail {
				return identity.Principal{}, errors.New("expired token")
			}
			return identity.Principal{TenantID: "tenantA", UserID: "u", Role: identity.RoleUser}, nil
		})
		middleware, err := HostPrincipal(auth)
		if err != nil {
			t.Fatal(err)
		}
		r := gin.New()
		r.Use(middleware)
		r.GET("/", func(c *gin.Context) {
			p, ok := identity.FromContext(c.Request.Context())
			if !ok || p.TenantID != "tenantA" || p.Role != identity.RoleUser {
				t.Errorf("forged authority: %+v", p)
			}
			c.Status(200)
		})
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Tenant-ID", "tenantB")
		req.Header.Set("X-Role", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 200
		if fail {
			want = 401
		}
		if w.Code != want {
			t.Fatalf("status %d want %d", w.Code, want)
		}
	}
	if _, err := HostPrincipal(nil); err == nil {
		t.Fatal("host without adapter accepted")
	}
}

func TestHostPrincipalPreservesExplicitForbidden(t *testing.T) {
	middleware, err := HostPrincipal(testHostAuth(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{}, commonerrors.ErrForbidden
	}))
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(middleware)
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != commonerrors.ErrForbidden.Code() {
		t.Fatalf("code = %d, want %d", envelope.Code, commonerrors.ErrForbidden.Code())
	}
}

func TestAnonymousPrincipalCannotBecomeAdmin(t *testing.T) {
	for _, userID := range []string{"visitor", ""} {
		attach, err := AnonymousPrincipal("fixed-tenant")
		if err != nil {
			t.Fatal(err)
		}
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(bizctx.WithKV(c.Request.Context(), bizctx.UserID(userID)))
		}, attach)
		r.GET("/", func(c *gin.Context) {
			p, ok := identity.FromContext(c.Request.Context())
			if !ok || p.TenantID != "fixed-tenant" || p.Role != identity.RoleUser {
				t.Fatalf("unexpected principal %+v", p)
			}
			c.Status(200)
		})
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Role", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 200
		if userID == "" {
			want = 401
		}
		if w.Code != want {
			t.Fatalf("status %d want %d", w.Code, want)
		}
	}
	if _, err := AnonymousPrincipal(""); err == nil {
		t.Fatal("implicit anonymous tenant accepted")
	}
}
