package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSameOriginAllowsSafeAndLocalRequests(t *testing.T) {
	router := gin.New()
	router.Use(SameOrigin())
	router.Any("/test", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	tests := []struct {
		method  string
		host    string
		origin  string
		referer string
	}{
		{method: http.MethodGet, host: "example.test", origin: "https://evil.test"},
		{method: http.MethodHead, host: "example.test"},
		{method: http.MethodOptions, host: "example.test"},
		{method: http.MethodPost, host: "127.0.0.1:8080"},
		{method: http.MethodPost, host: "localhost:8080"},
		{method: http.MethodPost, host: "127.0.0.1:8080", origin: "http://127.0.0.1:8080"},
		{method: http.MethodPost, host: "127.0.0.1:8080", referer: "http://127.0.0.1:8080/chat"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, "/test", nil)
		request.Host = test.host
		request.Header.Set("Origin", test.origin)
		request.Header.Set("Referer", test.referer)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Errorf("%s host=%q origin=%q referer=%q status=%d", test.method, test.host, test.origin, test.referer, response.Code)
		}
	}
}

func TestSameOriginRejectsCrossOriginAndNonLocalHeaderlessPosts(t *testing.T) {
	router := gin.New()
	router.Use(SameOrigin())
	router.POST("/test", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	tests := []struct {
		host, origin, referer string
	}{
		{host: "127.0.0.1:8080", origin: "https://evil.test"},
		{host: "127.0.0.1:8080", origin: "null"},
		{host: "127.0.0.1:8080", referer: "://bad"},
		{host: "example.test"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodPost, "/test", nil)
		request.Host = test.host
		request.Header.Set("Origin", test.origin)
		request.Header.Set("Referer", test.referer)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("host=%q origin=%q referer=%q status=%d", test.host, test.origin, test.referer, response.Code)
		}
	}
}
