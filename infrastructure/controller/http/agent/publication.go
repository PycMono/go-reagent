package agent

import (
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
)

func RegisterPublicationRoutes(r *gin.Engine, service *agentversion.PublicationService, cfg *config.Config) {
	if !cfg.Identity.AdminEnabled() {
		return
	}
	admin := r.Group("/api/v1", ManagementGuard())
	admin.POST("/agents/:agentID/model-config-releases", func(c *gin.Context) {
		p, err := identity.Require(c.Request.Context())
		if err != nil {
			Send(c, nil, err)
			return
		}
		var in dto.ModelConfigReleaseDTO
		if err := Decode(c, &in); err != nil {
			Send(c, nil, err)
			return
		}
		data, err := service.ReleaseModel(c.Request.Context(), p, c.Param("agentID"), in)
		Send(c, data, err)
	})
	admin.POST("/agents/:agentID/versions/:versionID/activate", func(c *gin.Context) {
		p, err := identity.Require(c.Request.Context())
		if err != nil {
			Send(c, nil, err)
			return
		}
		var in struct {
			ExpectedRowVersion uint64 `json:"expected_row_version,string"`
		}
		if err := Decode(c, &in); err != nil {
			Send(c, nil, err)
			return
		}
		data, err := service.Activate(c.Request.Context(), p, c.Param("agentID"), c.Param("versionID"), in.ExpectedRowVersion)
		Send(c, data, err)
	})
}
