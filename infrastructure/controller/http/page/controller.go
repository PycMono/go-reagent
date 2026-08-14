package page

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Controller struct {
	renderer *Renderer
}

func NewController(renderer *Renderer) *Controller { return &Controller{renderer: renderer} }

func (ctl *Controller) Chat(c *gin.Context) {
	content, err := ctl.renderer.Render("chat.html", gin.H{
		"Title": "Reagent Chat",
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "template render error")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
}
