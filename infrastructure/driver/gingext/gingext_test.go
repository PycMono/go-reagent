package gingext

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PycMono/go-gin-sdk/session"
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
)

type engineAuth func(*http.Request) (identity.Principal, error)

func (auth engineAuth) Authenticate(request *http.Request) (identity.Principal, error) {
	return auth(request)
}

type engineSessions struct {
	loads, creates int
	userID         string
}

func (store *engineSessions) Load(context.Context, string) (*session.Session, error) {
	store.loads++
	return nil, session.ErrSessionNotFound
}

func (store *engineSessions) Create(_ context.Context, data session.Session) (string, error) {
	store.creates++
	store.userID = data.UserID
	return "session-id", nil
}

func TestNewEngineRequiresExplicitIdentityIntegration(t *testing.T) {
	store := &engineSessions{}
	if _, err := newEngine(&config.Config{}, store, nil); err == nil {
		t.Fatal("server accepted missing identity configuration")
	}
	if _, err := newEngine(&config.Config{Identity: config.IdentityConfig{Mode: "host"}}, store, nil); err == nil {
		t.Fatal("host mode accepted missing Authenticator")
	}
}

func TestHostEngineTrustsOnlyAuthenticator(t *testing.T) {
	store := &engineSessions{}
	want := identity.Principal{TenantID: "trusted", UserID: "host-user", Role: identity.RoleAdmin}
	router, err := newEngine(&config.Config{Identity: config.IdentityConfig{Mode: "host"}}, store, engineAuth(func(*http.Request) (identity.Principal, error) {
		return want, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	registerPrincipalProbe(router)
	request := httptest.NewRequest(http.MethodGet, "/app", nil)
	request.Header.Set("X-Tenant-ID", "forged")
	request.Header.Set("X-Role", "user")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Test-Principal") != "trusted/host-user/admin" {
		t.Fatalf("status=%d principal=%q", response.Code, response.Header().Get("X-Test-Principal"))
	}
	if store.loads != 0 || store.creates != 0 {
		t.Fatal("host mode invoked Visitor")
	}
}

func TestHostEngineFailureDoesNotFallBackToVisitor(t *testing.T) {
	store := &engineSessions{}
	router, err := newEngine(&config.Config{Identity: config.IdentityConfig{Mode: "host"}}, store, engineAuth(func(*http.Request) (identity.Principal, error) {
		return identity.Principal{}, errors.New("invalid credential")
	}))
	if err != nil {
		t.Fatal(err)
	}
	registerPrincipalProbe(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/app", nil))
	if response.Code != http.StatusUnauthorized || store.loads != 0 || store.creates != 0 {
		t.Fatalf("status=%d visitor=%d/%d", response.Code, store.loads, store.creates)
	}
}

func TestAnonymousEngineUsesFixedTenantAndVisitorUser(t *testing.T) {
	store := &engineSessions{}
	router, err := newEngine(&config.Config{Identity: config.IdentityConfig{Mode: "anonymous", TenantID: "public"}}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	registerPrincipalProbe(router)
	request := httptest.NewRequest(http.MethodGet, "/app", nil)
	request.Header.Set("X-Tenant-ID", "forged")
	request.Header.Set("X-Role", "admin")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Test-Principal") != "public/"+store.userID+"/user" {
		t.Fatalf("status=%d principal=%q", response.Code, response.Header().Get("X-Test-Principal"))
	}
	if store.creates != 1 {
		t.Fatalf("visitor creates=%d", store.creates)
	}
}

func TestPublicRoutesBypassIdentityAndVisitor(t *testing.T) {
	store := &engineSessions{}
	authCalls := 0
	router, err := newEngine(&config.Config{Identity: config.IdentityConfig{Mode: "host"}}, store, engineAuth(func(*http.Request) (identity.Principal, error) {
		authCalls++
		return identity.Principal{}, errors.New("must not run")
	}))
	if err != nil {
		t.Fatal(err)
	}
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/static/app.js", func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, path := range []string{"/health", "/static/app.js"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, response.Code)
		}
	}
	if authCalls != 0 || store.loads != 0 || store.creates != 0 {
		t.Fatalf("public route side effects auth=%d visitor=%d/%d", authCalls, store.loads, store.creates)
	}
}

func registerPrincipalProbe(router *gin.Engine) {
	router.GET("/app", func(c *gin.Context) {
		principal, ok := identity.FromContext(c.Request.Context())
		if !ok {
			c.Status(http.StatusUnauthorized)
			return
		}
		c.Header("X-Test-Principal", principal.TenantID+"/"+principal.UserID+"/"+string(principal.Role))
		c.Status(http.StatusOK)
	})
}
