package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chatctl "github.com/PycMono/go-reagent/infrastructure/controller/http/chat"
	pagectl "github.com/PycMono/go-reagent/infrastructure/controller/http/page"
	"github.com/gin-gonic/gin"
)

func TestRegisterRoutesExposesChatAPIAndHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, &chatctl.Controller{})
	want := map[string]bool{
		"GET /health":                                        true,
		"GET /api/v1/agent-profiles":                         true,
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

func TestRegisterPageRoutesServesEmbeddedStaticAssets(t *testing.T) {
	renderer, err := pagectl.NewProductionRenderer()
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterPageRoutes(router, pagectl.NewController(renderer))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/static/js/pages/chat.js", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `const PROFILE_API = "/api/v1/agent-profiles"`) {
		t.Fatalf("static response = %d / %s", response.Code, response.Body.String())
	}
}
