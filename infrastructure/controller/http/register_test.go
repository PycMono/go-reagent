package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/config"
	chatctl "github.com/PycMono/go-reagent/infrastructure/controller/http/chat"
	pagectl "github.com/PycMono/go-reagent/infrastructure/controller/http/page"
	"github.com/PycMono/go-reagent/infrastructure/middleware"
	"github.com/gin-gonic/gin"
)

func TestRegisterRoutesExposesChatAPIAndHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, &chatctl.Controller{})
	want := map[string]bool{
		"GET /health":                                        true,
		"GET /api/v1/conversations/:id":                      true,
		"POST /api/v1/conversations":                         true,
		"GET /api/v1/conversations":                          true,
		"PATCH /api/v1/conversations/:id":                    true,
		"DELETE /api/v1/conversations/:id":                   true,
		"GET /api/v1/conversations/:id/messages":             true,
		"POST /api/v1/conversations/:id/runs":                true,
		"POST /api/v1/conversations/:id/runs/:run_id/cancel": true,
	}
	for _, route := range router.Routes() {
		delete(want, route.Method+" "+route.Path)
	}
	if len(want) != 0 {
		t.Fatalf("missing routes = %#v", want)
	}
}

func TestAgentDirectoryIsDefaultAndChatRequiresTarget(t *testing.T) {
	renderer, err := pagectl.NewProductionRenderer()
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	RegisterPageRoutes(r, pagectl.NewController(renderer))
	for _, tc := range []struct {
		path           string
		status         int
		location, body string
	}{{"/", 302, "/agents", ""}, {"/agents", 200, "", `id="agentList"`}, {"/chat", 302, "/agents", ""}, {"/chat?agent_id=a", 200, "", `id="chatComposer"`}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status || w.Header().Get("Location") != tc.location || !strings.Contains(w.Body.String(), tc.body) {
			t.Fatalf("%s status=%d location=%s", tc.path, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestRegisterPageRoutesServesEmbeddedStaticAssets(t *testing.T) {
	renderer, err := pagectl.NewProductionRenderer()
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterPageRoutes(router, pagectl.NewController(renderer))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/static/js/pages/chat.js", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `const AGENT_API = "/api/v1/agents"`) ||
		strings.Contains(body, "/api/v1/agent-profiles") || strings.Contains(body, "defaultProfile") {
		t.Fatalf("static response = %d / %s", response.Code, response.Body.String())
	}
}

func TestRegisterAdminRoutesGatedByIdentityConfig(t *testing.T) {
	renderer, err := pagectl.NewProductionRenderer()
	if err != nil {
		t.Fatal(err)
	}
	pageController := pagectl.NewController(renderer)
	for _, tt := range []struct {
		name        string
		token       string
		adminPages  bool
		devLogin    bool
		loginStatus int
	}{
		{name: "disabled by default", token: ""},
		{name: "anonymous dev token", token: "local-dev-admin-0001", adminPages: true, devLogin: true, loginStatus: http.StatusFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			RegisterAdminRoutes(router, pageController, &config.Config{Identity: config.IdentityConfig{
				Mode: config.IdentityModeAnonymous, TenantID: "tenant-a", DevAdminToken: tt.token,
			}})
			registered := map[string]bool{}
			for _, route := range router.Routes() {
				registered[route.Method+" "+route.Path] = true
			}
			if registered["GET /admin/agents"] != tt.adminPages || registered["GET /dev/login"] != tt.devLogin {
				t.Fatalf("routes = %#v", registered)
			}
			if tt.loginStatus == 0 {
				return
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/dev/login?token=wrong", nil))
			if response.Code != http.StatusForbidden {
				t.Fatalf("wrong token status = %d, want 403", response.Code)
			}
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/dev/login?token="+tt.token, nil))
			if response.Code != http.StatusFound || response.Header().Get("Location") != "/admin/agents" {
				t.Fatalf("login status = %d location = %q", response.Code, response.Header().Get("Location"))
			}
			cookies := response.Result().Cookies()
			if len(cookies) == 0 || cookies[0].Name != middleware.DevAdminCookieName || cookies[0].Value != tt.token {
				t.Fatalf("login cookies = %#v", cookies)
			}
		})
	}
}
