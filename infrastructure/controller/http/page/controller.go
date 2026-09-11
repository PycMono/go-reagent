package page

import (
	"net/http"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/gin-gonic/gin"
)

type Controller struct {
	renderer *Renderer
}

func NewController(renderer *Renderer) *Controller { return &Controller{renderer: renderer} }

func (ctl *Controller) Chat(c *gin.Context) {
	if c.Request.URL.Path == "/chat" && c.Query("agent_id") == "" && c.Query("conversation_id") == "" {
		c.Redirect(http.StatusFound, "/agents")
		return
	}
	content, err := ctl.renderer.Render("chat.html", gin.H{
		"Title": "Reagent Chat",
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "template render error")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
}

func (ctl *Controller) Agents(c *gin.Context) {
	p, ok := identity.FromContext(c.Request.Context())
	content, err := ctl.renderer.Render("agents.html", gin.H{"Title": "Agents", "TenantLabel": p.TenantID, "CanManage": ok && p.Role == identity.RoleAdmin})
	if err != nil {
		c.String(http.StatusInternalServerError, "template render error")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
}
