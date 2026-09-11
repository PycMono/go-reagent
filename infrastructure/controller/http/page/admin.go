package page

import (
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (ctl *Controller) renderAdmin(c *gin.Context, mode, title string) {
	p, err := identity.Require(c.Request.Context())
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if p.RequireAdmin() != nil {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	template := "admin-agents.html"
	if mode == "training" {
		template = "training.html"
	}
	content, err := ctl.renderer.Render(template, gin.H{"Title": title, "Mode": mode, "AgentID": c.Param("agentID"), "TrainingID": c.Param("trainingID"), "TenantLabel": p.TenantID})
	if err != nil {
		c.String(http.StatusInternalServerError, "template render error")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
}
func (ctl *Controller) AdminAgents(c *gin.Context)      { ctl.renderAdmin(c, "list", "管理 Agent") }
func (ctl *Controller) AdminAgentCreate(c *gin.Context) { ctl.renderAdmin(c, "create", "创建 Agent") }
func (ctl *Controller) AdminAgentDetail(c *gin.Context) { ctl.renderAdmin(c, "detail", "Agent 详情") }

func (ctl *Controller) AdminTraining(c *gin.Context) {
	ctl.renderAdmin(c, "training", "训练工作台")
}

// RegisterAdminPageRoutes is only installed by the host-identity composition root.
func RegisterAdminPageRoutes(r *gin.Engine, ctl *Controller) {
	r.GET("/admin/training-sessions/:trainingID", ctl.AdminTraining)
	r.GET("/admin/agents", ctl.AdminAgents)
	r.GET("/admin/agents/new", ctl.AdminAgentCreate)
	r.GET("/admin/agents/:agentID", ctl.AdminAgentDetail)
}
