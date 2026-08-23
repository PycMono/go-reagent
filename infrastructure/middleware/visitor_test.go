package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/PycMono/go-context-sdk/bizctx"
	"github.com/PycMono/go-gin-sdk/session"
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
)

// fakeSessionStore 是 sessionStore 的内存实现。
type fakeSessionStore struct {
	sessions map[string]*session.Session
	next     int
	loadErr  error
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: map[string]*session.Session{}}
}

func (s *fakeSessionStore) Load(_ context.Context, sid string) (*session.Session, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	sess, found := s.sessions[sid]
	if !found {
		return nil, session.ErrSessionNotFound
	}
	return sess, nil
}

func (s *fakeSessionStore) Create(_ context.Context, data session.Session) (string, error) {
	s.next++
	sid := string(data.Type) + "." + strconv.Itoa(s.next)
	s.sessions[sid] = &data
	return sid, nil
}

func newVisitorRouter(conf *config.Config, store sessionStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Visitor(conf, store))
	router.GET("/who", func(c *gin.Context) { c.String(http.StatusOK, bizctx.GetUserID(c.Request.Context())) })
	return router
}

func TestVisitorCreatesAndReusesSessionIdentity(t *testing.T) {
	store := newFakeSessionStore()
	router := newVisitorRouter(&config.Config{}, store)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/who", nil))
	cookies := first.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != VisitorCookieName || cookie.Value == "" || !cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != visitorCookieMaxAge {
		t.Fatalf("cookie = %#v", cookie)
	}
	userID := first.Body.String()
	if userID == "" || userID == cookie.Value {
		t.Fatalf("userID 应与 sid 分离：userID = %q, sid = %q", userID, cookie.Value)
	}

	secondRequest := httptest.NewRequest(http.MethodGet, "/who", nil)
	secondRequest.AddCookie(cookie)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, secondRequest)
	if second.Body.String() != userID || len(second.Result().Cookies()) != 0 {
		t.Fatalf("replayed identity = %q, want %q; cookies = %#v", second.Body.String(), userID, second.Result().Cookies())
	}
}

func TestVisitorRenewsExpiredOrUnknownSession(t *testing.T) {
	store := newFakeSessionStore()
	router := newVisitorRouter(&config.Config{}, store)

	request := httptest.NewRequest(http.MethodGet, "/who", nil)
	request.AddCookie(&http.Cookie{Name: VisitorCookieName, Value: "toc.expired"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == "toc.expired" || response.Body.String() == "" {
		t.Fatalf("过期会话应重新签发：cookies = %#v, body = %q", cookies, response.Body.String())
	}
}

func TestVisitorHonorsSecureCookieConfig(t *testing.T) {
	router := newVisitorRouter(&config.Config{HTTP: config.HTTPConfig{SecureCookies: true}}, newFakeSessionStore())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/who", nil))
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("secure cookies = %#v", cookies)
	}
}

func TestVisitorAbortsWhenSessionStoreFails(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["toc.known"] = &session.Session{UserID: "user"}
	store.loadErr = errors.New("redis down")
	router := newVisitorRouter(&config.Config{}, store)

	request := httptest.NewRequest(http.MethodGet, "/who", nil)
	request.AddCookie(&http.Cookie{Name: VisitorCookieName, Value: "toc.known"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
}

func TestVisitorIssuesDifferentIdentitiesAcrossBrowsers(t *testing.T) {
	router := newVisitorRouter(&config.Config{}, newFakeSessionStore())
	identities := map[string]bool{}
	for range 2 {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/who", nil))
		identities[response.Body.String()] = true
	}
	if len(identities) != 2 {
		t.Fatalf("identities = %#v", identities)
	}
}
