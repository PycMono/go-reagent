package http

import (
	"testing"

	chatctl "github.com/PycMono/go-reagent/infrastructure/controller/http/chat"
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
