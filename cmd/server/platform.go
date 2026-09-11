package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"time"

	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-reagent/application/service/agentadmission"
	"github.com/PycMono/go-reagent/application/service/agentcatalog"
	"github.com/PycMono/go-reagent/application/service/agentruntime"
	"github.com/PycMono/go-reagent/application/service/agenttraining"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/domain/repository"
	agentrepo "github.com/PycMono/go-reagent/domain/repository/agent"
	profilerepo "github.com/PycMono/go-reagent/domain/repository/agentprofile"
	agentctl "github.com/PycMono/go-reagent/infrastructure/controller/http/agent"
	trainingctl "github.com/PycMono/go-reagent/infrastructure/controller/http/agenttraining"
	bundledriver "github.com/PycMono/go-reagent/infrastructure/driver/agentbundle"
	cursordriver "github.com/PycMono/go-reagent/infrastructure/driver/agentcatalog"
	smokedriver "github.com/PycMono/go-reagent/infrastructure/driver/agentsmoke"
	statedriver "github.com/PycMono/go-reagent/infrastructure/driver/agentstate"
	templatedriver "github.com/PycMono/go-reagent/infrastructure/driver/agenttemplate"
	dbtime "github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	agentpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/agent"
	trainingpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/agenttraining"
	"github.com/PycMono/go-reagent/pi"
	"go.uber.org/fx"
)

// The Web application has no shared model runtime. Every admitted conversation
// resolves its tenant-owned immutable version through the bounded manager.
var platformRegister = fx.Options(
	fx.Provide(agentpersistence.NewRepository),
	fx.Provide(func(r *agentpersistence.Repository) agentrepo.Repository { return r }),
	fx.Provide(newPlatformOwnership, newPlatformBundles, newPlatformJournal, newPlatformTemplates,
		newPlatformValidator, newPlatformBuilder, newPlatformCatalog,
		newPlatformRuntimes, chatservice.NewBoundService, agentctl.NewController,
		agentadmission.New, trainingpersistence.NewRepository, newPlatformTraining, newPlatformPublication),
	fx.Invoke(agentctl.RegisterRoutes, agentctl.RegisterPublicationRoutes, trainingctl.RegisterRoutes),
)

func newPlatformTraining(r *trainingpersistence.Repository, a agentrepo.Repository, b *bundledriver.Store, v *agentversion.Validator, m *agentruntime.Manager, admission *agentadmission.Coordinator, c *config.Config, ids repository.IIDService) (*agenttraining.Service, error) {
	return agenttraining.NewService(r, a, b, v, &agenttraining.RuntimeAuthor{Runtimes: m, Bundles: b}, admission, c.AgentTraining, ids, c)
}

func newPlatformPublication(r *agentpersistence.Repository, b *bundledriver.Store, v *agentversion.Validator, c *config.Config, a *agentadmission.Coordinator, ids repository.IIDService) (*agentversion.PublicationService, error) {
	return agentversion.NewPublicationService(r, b, b, v, c, a, ids)
}

type platformOwnership struct{}

func newPlatformOwnership(lc fx.Lifecycle, c *config.Config) *platformOwnership {
	var lock *os.File
	lc.Append(fx.Hook{OnStart: func(context.Context) error { var err error; lock, err = statedriver.Lock(c.AgentDataDir); return err }, OnStop: func(context.Context) error {
		if lock != nil {
			return lock.Close()
		}
		return nil
	}})
	return &platformOwnership{}
}

func newPlatformBundles(c *config.Config, _ *platformOwnership) (*bundledriver.Store, error) {
	if !c.Conversation.Enabled {
		return nil, errors.New("multi-Agent server requires MySQL conversation persistence")
	}
	return bundledriver.New(c.AgentDataDir)
}
func newPlatformJournal(c *config.Config) (*statedriver.Journal, error) {
	return statedriver.New(c.AgentDataDir)
}
func newPlatformTemplates(w pi.WorkDir, p profilerepo.Catalog) (*templatedriver.Assembler, error) {
	return templatedriver.New(string(w), p)
}

func newPlatformRuntimes(lc fx.Lifecycle, p appParams, b *bundledriver.Store) (*agentruntime.Manager, error) {
	r := p.Config.AgentRuntime
	m, err := agentruntime.NewManager(&agentruntime.PIFactory{Config: p.Config, Bundles: b, Tools: p.ChatTools, Notifiers: p.Notifiers}, agentruntime.Options{
		MaxInstances: r.MaxInstances, MaxInstancesPerTenant: r.MaxInstancesPerTenant,
		ValidationReservedInstances: r.ValidationReservedInstances, ValidationReservedInstancesPerTenant: r.ValidationReservedInstancesPerTenant,
		IdleTTL: time.Duration(r.IdleTTLSeconds) * time.Second, AcquireTimeout: time.Duration(r.AcquireTimeoutSeconds) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: m.Close})
	return m, nil
}
func newPlatformValidator(p appParams, b *bundledriver.Store, db sqlsdk.Provider, m *agentruntime.Manager, ids repository.IIDService) (*agentversion.Validator, error) {
	refs := func(ctx context.Context, s agentversion.Snapshot) error {
		if _, err := p.Config.ResolveAgentModel(s.Model); err != nil {
			return err
		}
		for _, ref := range s.Tools.MCP {
			if _, err := p.Config.ResolveAgentMCP(ref); err != nil {
				return err
			}
		}
		available := map[string]bool{}
		for _, tool := range p.ChatTools {
			available[tool.Definition().Name] = true
		}
		for _, ref := range s.Tools.Registered {
			if !available[ref.Name] || ref.ImplementationRef != "application:"+ref.Name+":v1" || len(ref.Arguments) != 0 {
				return errors.New("saved application tool unavailable")
			}
		}
		return ctx.Err()
	}
	clock := func(ctx context.Context) (time.Time, error) {
		conn := db.UseDB(ctx)
		if conn == nil {
			return time.Time{}, errors.New("database UTC clock unavailable")
		}
		return dbtime.UTCNow(conn.WithContext(ctx))
	}
	smoke, err := smokedriver.New(p.Config.AgentTraining)
	if err != nil {
		return nil, err
	}
	policy, _ := json.Marshal(p.Config.AgentTraining.ApprovedInterpreters)
	return agentversion.NewValidator(b, refs, smoke.Check, clock, agentversion.ValidationPolicy{Version: "1", Environment: "isolated-syntax-v1:" + runtime.GOOS + ":" + string(policy), Timeout: time.Duration(p.Config.AgentTraining.SmokeTimeoutSeconds) * time.Second, TTL: time.Duration(p.Config.AgentTraining.ValidationTTLSeconds) * time.Second}, func(ctx context.Context, tenant, agent string) (func(), error) {
		return m.ReserveValidation(ctx, tenant, agent, ids.NextID())
	})
}
func newPlatformBuilder(p appParams, b *bundledriver.Store, j *statedriver.Journal, t *templatedriver.Assembler, v *agentversion.Validator) (*agentversion.InitialBuilder, error) {
	names := []string{}
	for _, tool := range p.ChatTools {
		names = append(names, tool.Definition().Name)
	}
	return agentversion.NewInitialBuilder(b, j, p.Config, t, v, names)
}
func newPlatformCatalog(c *config.Config, r agentrepo.Repository, b *agentversion.InitialBuilder, p profilerepo.Catalog, ids repository.IIDService) (*agentcatalog.Service, error) {
	key, err := cursordriver.LoadCursorKey(c.AgentDataDir)
	if err != nil {
		return nil, err
	}
	return agentcatalog.NewService(r, b, p, ids, key)
}
