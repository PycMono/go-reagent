package gingext

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PycMono/go-context-sdk/bizctx"
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
)

func TestNewEngineInstallsVisitorAndSameOriginBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := NewEngine(&config.Config{})
	engine.GET("/who", func(c *gin.Context) { c.String(http.StatusOK, bizctx.GetUserID(c.Request.Context())) })
	engine.POST("/write", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	who := httptest.NewRecorder()
	engine.ServeHTTP(who, httptest.NewRequest(http.MethodGet, "/who", nil))
	if who.Body.String() == "" || len(who.Result().Cookies()) != 1 {
		t.Fatalf("visitor response = %q, cookies = %#v", who.Body.String(), who.Result().Cookies())
	}

	request := httptest.NewRequest(http.MethodPost, "/write", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Origin", "https://evil.test")
	blocked := httptest.NewRecorder()
	engine.ServeHTTP(blocked, request)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", blocked.Code)
	}
}
