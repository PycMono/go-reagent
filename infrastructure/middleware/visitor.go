package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"

	"github.com/PycMono/go-context-sdk/bizctx"
	"github.com/PycMono/go-gin-sdk/session"
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
)

const (
	VisitorCookieName   = "reagent_visitor"
	visitorIDByteSize   = 32
	visitorCookieMaxAge = 5 * 24 * 60 * 60 // 与 go-gin-sdk session 默认 TTL 对齐
)

// sessionStore 是访客会话存储的最小接口；*session.Manager 直接满足，
// 测试用内存实现。
type sessionStore interface {
	Load(ctx context.Context, sid string) (*session.Session, error)
	Create(ctx context.Context, data session.Session) (string, error)
}

// Visitor 为每个浏览器 Cookie Jar 分配匿名身份（免登录）：Cookie 里的 sid
// 指向 go-gin-sdk session；无 Cookie、会话不存在或已过期时自动创建匿名会话。
// Cookie 中是可注销的服务端会话句柄，UserID 本体只在服务端存储。
func Visitor(conf *config.Config, store sessionStore) gin.HandlerFunc {
	return visitorWithReader(conf, store, rand.Reader)
}

func visitorWithReader(conf *config.Config, store sessionStore, random io.Reader) gin.HandlerFunc {
	secure := conf != nil && conf.HTTP.SecureCookies
	return func(c *gin.Context) {
		if sid, err := c.Cookie(VisitorCookieName); err == nil && sid != "" {
			sess, err := store.Load(c.Request.Context(), sid)
			switch {
			case err == nil && sess.UserID != "":
				c.Request = c.Request.WithContext(bizctx.WithKV(c.Request.Context(), bizctx.UserID(sess.UserID)))
				c.Next()
				return
			case err != nil && !errors.Is(err, session.ErrSessionNotFound):
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code": 10009, "msg": "internal server error", "data": struct{}{},
				})
				return
			}
			// 会话不存在或已过期：落入下方重新签发。
		}

		userID, err := newVisitorID(random)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"code": 10009, "msg": "internal server error", "data": struct{}{},
			})
			return
		}
		sid, err := store.Create(c.Request.Context(), session.Session{Type: session.TypeUser, UserID: userID})
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"code": 10009, "msg": "internal server error", "data": struct{}{},
			})
			return
		}
		http.SetCookie(c.Writer, &http.Cookie{
			Name: VisitorCookieName, Value: sid, Path: "/",
			MaxAge: visitorCookieMaxAge,
			HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		})
		c.Request = c.Request.WithContext(bizctx.WithKV(c.Request.Context(), bizctx.UserID(userID)))
		c.Next()
	}
}

func newVisitorID(random io.Reader) (string, error) {
	content := make([]byte, visitorIDByteSize)
	if _, err := io.ReadFull(random, content); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(content), nil
}
