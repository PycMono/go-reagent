package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// SameOrigin rejects browser state changes originating outside this server.
func SameOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if safeMethod(c.Request.Method) || requestIsSameOrigin(c.Request) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"code": 10003, "msg": "forbidden", "data": struct{}{},
		})
	}
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func requestIsSameOrigin(request *http.Request) bool {
	source := strings.TrimSpace(request.Header.Get("Origin"))
	if source == "" {
		source = strings.TrimSpace(request.Header.Get("Referer"))
	}
	if source == "" {
		return loopbackHost(request.Host)
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.IsAbs() == false || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	return strings.EqualFold(normalizeAuthority(parsed.Host), normalizeAuthority(request.Host))
}

func normalizeAuthority(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func loopbackHost(authority string) bool {
	host := strings.TrimSpace(authority)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	} else {
		host = strings.Trim(host, "[]")
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
