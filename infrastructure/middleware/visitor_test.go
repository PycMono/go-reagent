package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PycMono/go-context-sdk/bizctx"
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
)

func TestVisitorCookieCreatesAndReusesBrowserIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Visitor(&config.Config{}))
	router.GET("/who", func(c *gin.Context) { c.String(http.StatusOK, bizctx.GetUserID(c.Request.Context())) })

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/who", nil))
	cookies := first.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != VisitorCookieName || cookie.Value == "" || !cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != visitorCookieMaxAge ||
		first.Body.String() != cookie.Value {
		t.Fatalf("cookie/body = %#v / %q", cookie, first.Body.String())
	}

	secondRequest := httptest.NewRequest(http.MethodGet, "/who", nil)
	secondRequest.AddCookie(cookie)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, secondRequest)
	if second.Body.String() != cookie.Value || len(second.Result().Cookies()) != 0 {
		t.Fatalf("replayed identity = %q, cookies = %#v", second.Body.String(), second.Result().Cookies())
	}
}

func TestVisitorCookieRotatesMalformedValuesAndHonorsSecureConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Visitor(&config.Config{HTTP: config.HTTPConfig{SecureCookies: true}}))
	router.GET("/who", func(c *gin.Context) { c.String(http.StatusOK, bizctx.GetUserID(c.Request.Context())) })
	for _, invalid := range []string{"short", "contains+invalid", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		request := httptest.NewRequest(http.MethodGet, "/who", nil)
		request.AddCookie(&http.Cookie{Name: VisitorCookieName, Value: invalid})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Value == invalid || !cookies[0].Secure || response.Body.String() != cookies[0].Value {
			t.Fatalf("invalid %q response = %#v / %q", invalid, cookies, response.Body.String())
		}
	}
}

func TestVisitorCookieIsDifferentAcrossBrowsers(t *testing.T) {
	router := gin.New()
	router.Use(Visitor(&config.Config{}))
	router.GET("/who", func(c *gin.Context) { c.String(http.StatusOK, bizctx.GetUserID(c.Request.Context())) })
	values := map[string]bool{}
	for range 2 {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/who", nil))
		values[response.Body.String()] = true
	}
	if len(values) != 2 {
		t.Fatalf("browser identities = %#v", values)
	}
}
