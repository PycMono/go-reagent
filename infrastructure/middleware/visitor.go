package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"time"

	"github.com/PycMono/go-context-sdk/bizctx"
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
)

const (
	VisitorCookieName   = "reagent_visitor"
	visitorIDByteSize   = 32
	visitorCookieMaxAge = 365 * 24 * 60 * 60
)

// Visitor assigns one opaque user identity to each browser cookie jar.
func Visitor(conf *config.Config) gin.HandlerFunc {
	return visitorWithReader(conf, rand.Reader)
}

func visitorWithReader(conf *config.Config, random io.Reader) gin.HandlerFunc {
	secure := conf != nil && conf.HTTP.SecureCookies
	return func(c *gin.Context) {
		visitorID, err := c.Cookie(VisitorCookieName)
		if err != nil || !validVisitorID(visitorID) {
			visitorID, err = newVisitorID(random)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code": 10009, "msg": "internal server error", "data": struct{}{},
				})
				return
			}
			http.SetCookie(c.Writer, &http.Cookie{
				Name: VisitorCookieName, Value: visitorID, Path: "/",
				MaxAge: visitorCookieMaxAge, Expires: time.Now().Add(time.Duration(visitorCookieMaxAge) * time.Second),
				HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
			})
		}
		ctx := bizctx.WithKV(c.Request.Context(), bizctx.UserID(visitorID))
		c.Request = c.Request.WithContext(ctx)
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

func validVisitorID(value string) bool {
	content, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(content) == visitorIDByteSize &&
		base64.RawURLEncoding.EncodeToString(content) == value
}
