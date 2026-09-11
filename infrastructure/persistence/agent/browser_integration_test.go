//go:build integration

package agent_test

import (
	"context"
	"github.com/PycMono/go-reagent/common/dto"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/application/service/agentcatalog"
	"github.com/PycMono/go-reagent/application/service/agenttraining"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/frontend"
	agentctl "github.com/PycMono/go-reagent/infrastructure/controller/http/agent"
	trainingctl "github.com/PycMono/go-reagent/infrastructure/controller/http/agenttraining"
	"github.com/PycMono/go-reagent/infrastructure/controller/http/page"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentbundle"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentstate"
	"github.com/PycMono/go-reagent/infrastructure/driver/agenttemplate"
	"github.com/PycMono/go-reagent/infrastructure/persistence/agent"
	"github.com/PycMono/go-reagent/pi"
	"github.com/gin-gonic/gin"
)

// Optional manual browser QA against the same disposable MySQL/Git fixture.
// Identity is fixed only inside this test server; no production auth bypass exists.
func browserQA(t *testing.T, cfg *config.Config, repo *agent.Repository, store *agentbundle.Store, validator *agentversion.Validator, ids *trainingIDs, training *agenttraining.Service, publication *agentversion.PublicationService, p identity.Principal) {
	if os.Getenv("AGENT_BROWSER_QA") != "1" {
		return
	}
	work, err := filepath.Abs("../../../workspaces/chat")
	if err != nil {
		t.Fatal(err)
	}
	templates, err := agentprofile.NewCatalog(pi.WorkDir(work))
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := agenttemplate.New(work, templates)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := agentstate.New(cfg.AgentDataDir)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := agentversion.NewInitialBuilder(store, journal, cfg, assembler, validator, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := agentcatalog.NewService(repo, builder, templates, ids, []byte("integration-only-cursor-key-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Create(context.Background(), p, dto.CreateAgentDTO{Name: "资料分析助手", Description: "整理资料，说明依据", TemplateCode: "general"}); err != nil {
		t.Fatalf("browser fixture template create: %v", err)
	}
	cfg.Identity.Mode = config.IdentityModeHost
	r := gin.New()
	r.Use(gin.Recovery(), func(c *gin.Context) {
		c.Request = c.Request.WithContext(identity.WithPrincipal(c.Request.Context(), p))
		c.Next()
	})
	ctl := agentctl.NewController(catalog, cfg, training)
	agentctl.RegisterRoutes(r, ctl, cfg)
	agentctl.RegisterPublicationRoutes(r, publication, cfg)
	trainingctl.RegisterRoutes(r, training, cfg)
	renderer, err := page.NewProductionRenderer()
	if err != nil {
		t.Fatal(err)
	}
	pages := page.NewController(renderer)
	page.RegisterAdminPageRoutes(r, pages)
	r.GET("/agents", pages.Agents)
	r.StaticFS("/static", http.FS(frontend.Static))
	server := httptest.NewServer(r)
	defer server.Close()
	stopFile := "/tmp/go-reagent-browser-stop"
	_ = os.Remove(stopFile)
	if err := os.WriteFile("/tmp/go-reagent-browser-url", []byte(server.URL), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("Browser fixture:", server.URL)
	timer := time.NewTimer(15 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("browser QA timed out")
		case <-ticker.C:
			if _, err := os.Stat(stopFile); err == nil {
				return
			}
		}
	}
}
