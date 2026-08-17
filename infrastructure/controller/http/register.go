package http

import (
	"net/http"

	chatctl "github.com/PycMono/go-reagent/infrastructure/controller/http/chat"
	pagectl "github.com/PycMono/go-reagent/infrastructure/controller/http/page"
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
)

func RegisterRoutes(router *gin.Engine, chatCtl *chatctl.Controller) {
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET(v1Prefix+"/agent-profiles", chatCtl.ListAgentProfiles)
	conversations := router.Group(v1Prefix + "/conversations")
	conversations.POST("", chatCtl.CreateConversation)
	conversations.GET("", chatCtl.ListConversations)
	conversations.PATCH("/:id", chatCtl.RenameConversation)
	conversations.DELETE("/:id", chatCtl.DeleteConversation)
	conversations.GET("/:id/messages", chatCtl.ListMessages)
	conversations.POST("/:id/runs", chatCtl.StartRun)
	conversations.POST("/:id/runs/:run_id/cancel", chatCtl.CancelRun)
}

func RegisterPageRoutes(router *gin.Engine, pageCtl *pagectl.Controller) {
	router.GET("/", pageCtl.Chat)
	router.Static("/static", "frontend/static")
}
