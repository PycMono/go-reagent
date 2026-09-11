package http

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/frontend"
	chatctl "github.com/PycMono/go-reagent/infrastructure/controller/http/chat"
	pagectl "github.com/PycMono/go-reagent/infrastructure/controller/http/page"
	"github.com/PycMono/go-reagent/infrastructure/middleware"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

const v1Prefix = "/api/v1"

var Register = fx.Options(
	fx.Provide(chatctl.NewController),
	fx.Provide(pagectl.NewProductionRenderer),
	fx.Provide(pagectl.NewController),
	fx.Invoke(RegisterRoutes),
	fx.Invoke(RegisterPageRoutes),
	fx.Invoke(RegisterAdminRoutes),
)

func RegisterRoutes(router *gin.Engine, chatCtl *chatctl.Controller) {
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	conversations := router.Group(v1Prefix + "/conversations")
	conversations.POST("", chatCtl.CreateConversation)
	conversations.GET("", chatCtl.ListConversations)
	conversations.GET("/:id", chatCtl.GetConversation)
	conversations.PATCH("/:id", chatCtl.RenameConversation)
	conversations.DELETE("/:id", chatCtl.DeleteConversation)
	conversations.GET("/:id/messages", chatCtl.ListMessages)
	conversations.POST("/:id/runs", chatCtl.StartRun)
	conversations.POST("/:id/runs/:run_id/cancel", chatCtl.CancelRun)
}

func RegisterPageRoutes(router *gin.Engine, pageCtl *pagectl.Controller) {
	router.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/agents") })
	router.GET("/agents", pageCtl.Agents)
	router.GET("/chat", pageCtl.Chat)
	router.StaticFS("/static", http.FS(frontend.Static))
}

// RegisterAdminRoutes 安装管理页面与 dev 管理员登录。管理页面仅在管理能力
// 可用时注册（host 认证或 anonymous + dev_admin_token）；/dev/login 是
// anonymous 本地开发用浏览器换取管理员 Cookie 的唯一入口，未配置令牌时不注册。
func RegisterAdminRoutes(router *gin.Engine, pageCtl *pagectl.Controller, conf *config.Config) {
	if conf.Identity.AdminEnabled() {
		pagectl.RegisterAdminPageRoutes(router, pageCtl)
	}
	token := strings.TrimSpace(conf.Identity.DevAdminToken)
	if token == "" {
		return
	}
	expected := []byte(token)
	router.GET("/dev/login", func(c *gin.Context) {
		if subtle.ConstantTimeCompare([]byte(c.Query("token")), expected) != 1 {
			c.String(http.StatusForbidden, "invalid dev admin token")
			return
		}
		http.SetCookie(c.Writer, &http.Cookie{
			Name: middleware.DevAdminCookieName, Value: token, Path: "/",
			MaxAge:   int((24 * time.Hour).Seconds()) * 30,
			HttpOnly: true, Secure: conf.HTTP.SecureCookies, SameSite: http.SameSiteLaxMode,
		})
		c.Redirect(http.StatusFound, "/admin/agents")
	})
	router.GET("/dev/logout", func(c *gin.Context) {
		http.SetCookie(c.Writer, &http.Cookie{
			Name: middleware.DevAdminCookieName, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: conf.HTTP.SecureCookies, SameSite: http.SameSiteLaxMode,
		})
		c.Redirect(http.StatusFound, "/agents")
	})
}
