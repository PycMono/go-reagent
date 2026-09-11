package agent

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	ginsdk "github.com/PycMono/go-gin-sdk"
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/application/service/agentcatalog"
	"github.com/PycMono/go-reagent/application/service/agentruntime"
	"github.com/PycMono/go-reagent/application/service/agenttraining"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
)

type Controller struct {
	service  *agentcatalog.Service
	config   *config.Config
	training *agenttraining.Service
}

func NewController(service *agentcatalog.Service, config *config.Config, training *agenttraining.Service) *Controller {
	return &Controller{service, config, training}
}
func Send(c *gin.Context, data any, err error) {
	if err != nil {
		var coded commonerrors.CodeError
		if errors.Is(err, agentruntime.ErrBusy) {
			err = commonerrors.ErrBusy
		} else if !errors.As(err, &coded) {
			err = commonerrors.ErrInternal.Wrap(err)
		}
	}
	ginsdk.Send(c, data, err, commonerrors.HTTPStatusForCode)
}
func decode(c *gin.Context, out any) error {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	if err != nil {
		return commonerrors.ErrInvalidParam.Wrap(err)
	}
	if err := agentversion.DecodeStrict(body, out); err != nil {
		return commonerrors.ErrInvalidParam.Wrap(err)
	}
	return nil
}
func Decode(c *gin.Context, out any) error { return decode(c, out) }
func managementGuard() gin.HandlerFunc     { return ManagementGuard() }
func ManagementGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := identity.Require(c.Request.Context())
		if err == nil {
			err = p.RequireAdmin()
		}
		if err != nil {
			Send(c, nil, err)
			c.Abort()
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			origin := c.GetHeader("Origin")
			if origin != "" || c.GetHeader("Cookie") != "" {
				parsed, err := url.Parse(origin)
				scheme := "http"
				if c.Request.TLS != nil {
					scheme = "https"
				}
				if err != nil || parsed.Scheme != scheme || parsed.Host != c.Request.Host || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
					Send(c, nil, commonerrors.ErrForbidden)
					c.Abort()
					return
				}
			}
		}
		c.Next()
	}
}
func (ctl *Controller) List(c *gin.Context) {
	p, err := identity.Require(c.Request.Context())
	if err != nil {
		Send(c, nil, err)
		return
	}
	var q dto.ListAgentsQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Send(c, nil, commonerrors.ErrInvalidParam)
		return
	}
	data, err := ctl.service.List(c.Request.Context(), p, q)
	Send(c, data, err)
}
func (ctl *Controller) Get(c *gin.Context) {
	p, err := identity.Require(c.Request.Context())
	if err != nil {
		Send(c, nil, err)
		return
	}
	data, err := ctl.service.Get(c.Request.Context(), p, c.Param("agentID"))
	Send(c, data, err)
}
func (ctl *Controller) Create(c *gin.Context) {
	p, err := identity.Require(c.Request.Context())
	if err != nil {
		Send(c, nil, err)
		return
	}
	var in dto.CreateAgentDTO
	if err := decode(c, &in); err != nil {
		Send(c, nil, err)
		return
	}
	data, err := ctl.service.Create(c.Request.Context(), p, in)
	Send(c, data, err)
}
func (ctl *Controller) Patch(c *gin.Context) {
	p, err := identity.Require(c.Request.Context())
	if err != nil {
		Send(c, nil, err)
		return
	}
	var in dto.PatchAgentDTO
	if err := decode(c, &in); err != nil {
		Send(c, nil, err)
		return
	}
	if in.Status != nil && *in.Status == "archived" {
		if err := ctl.training.ReconcileAgent(c.Request.Context(), p, c.Param("agentID")); err != nil {
			Send(c, nil, err)
			return
		}
	}
	data, err := ctl.service.Patch(c.Request.Context(), p, c.Param("agentID"), in)
	Send(c, data, err)
}
func (ctl *Controller) Templates(c *gin.Context) {
	p, err := identity.Require(c.Request.Context())
	if err != nil {
		Send(c, nil, err)
		return
	}
	data, err := ctl.service.Templates(c.Request.Context(), p)
	Send(c, data, err)
}
func (ctl *Controller) Versions(c *gin.Context) {
	p, err := identity.Require(c.Request.Context())
	if err != nil {
		Send(c, nil, err)
		return
	}
	n := 0
	if raw, ok := c.GetQuery("limit"); ok {
		n, err = strconv.Atoi(raw)
		if err != nil || n < 1 {
			Send(c, nil, commonerrors.ErrInvalidParam)
			return
		}
	}
	data, err := ctl.service.Versions(c.Request.Context(), p, c.Param("agentID"), c.Query("cursor"), n)
	Send(c, data, err)
}
func (ctl *Controller) Models(c *gin.Context) {
	p, err := identity.Require(c.Request.Context())
	if err == nil {
		err = p.RequireAdmin()
	}
	if err != nil {
		Send(c, nil, err)
		return
	}
	items := []gin.H{}
	for _, model := range ctl.config.Platforms {
		if model.APIKey != "" {
			items = append(items, gin.H{"provider_ref": model.ID, "model_id": model.Model, "vision": model.Vision, "parameters": gin.H{}})
		}
	}
	Send(c, gin.H{"items": items}, nil)
}
func RegisterRoutes(r *gin.Engine, ctl *Controller, cfg *config.Config) {
	r.GET("/api/v1/agents", ctl.List)
	r.GET("/api/v1/agents/:agentID", ctl.Get)
	if !cfg.Identity.AdminEnabled() {
		return
	}
	admin := r.Group("/api/v1", ManagementGuard())
	admin.POST("/agents", ctl.Create)
	admin.PATCH("/agents/:agentID", ctl.Patch)
	admin.GET("/agent-templates", ctl.Templates)
	admin.GET("/agent-model-options", ctl.Models)
	admin.GET("/agents/:agentID/versions", ctl.Versions)
}
