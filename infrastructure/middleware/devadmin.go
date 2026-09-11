package middleware

import (
	"crypto/subtle"
	"errors"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/gin-gonic/gin"
)

// DevAdminCookieName 是 dev_admin_token 的浏览器 Cookie 名；令牌也可通过
// X-Dev-Admin-Token 请求头提供（便于命令行调用管理 API）。
const DevAdminCookieName = "reagent_dev_admin"

const devAdminTokenHeader = "X-Dev-Admin-Token"

// DevAdminUpgrade 在 anonymous 本地开发部署中，把持有正确令牌的普通访客
// 升级为管理员。仅当 identity.dev_admin_token 配置时安装；生产 host 部署
// 必须依赖宿主认证，不使用该机制。
func DevAdminUpgrade(token string) (gin.HandlerFunc, error) {
	if token == "" {
		return nil, errors.New("dev admin upgrade requires a configured token")
	}
	expected := []byte(token)
	return func(c *gin.Context) {
		supplied := c.GetHeader(devAdminTokenHeader)
		if supplied == "" {
			supplied, _ = c.Cookie(DevAdminCookieName)
		}
		if subtle.ConstantTimeCompare([]byte(supplied), expected) == 1 {
			if p, ok := identity.FromContext(c.Request.Context()); ok && p.Role == identity.RoleUser {
				p.Role = identity.RoleAdmin
				attachPrincipal(c, p)
			}
		}
		c.Next()
	}, nil
}