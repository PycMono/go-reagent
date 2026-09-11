// agent-migrate performs explicit, offline Agent provisioning and ownership
// backfill. It never infers a tenant from cookies or runs DDL automatically.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/application/service/agentcatalog"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	chattools "github.com/PycMono/go-reagent/application/tool/chat"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentbundle"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentsmoke"
	"github.com/PycMono/go-reagent/infrastructure/driver/agentstate"
	"github.com/PycMono/go-reagent/infrastructure/driver/agenttemplate"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	"github.com/PycMono/go-reagent/infrastructure/persistence/agent"
	"github.com/PycMono/go-reagent/infrastructure/serviceimpl"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"go.uber.org/fx"
)

func main() {
	mode := flag.String("mode", "check", "check, bootstrap, backfill, or verify")
	tenant := flag.String("tenant-id", "", "host-confirmed tenant ID")
	single := flag.Bool("confirmed-single-tenant-history", false, "operator confirms all legacy conversations belong to tenant-id")
	mapping := flag.String("mapping-file", "", "host-confirmed per-conversation ownership JSON")
	dry := flag.Bool("dry-run", true, "validate and report only; set false to apply backfill")
	flag.Parse()
	if err := run(context.Background(), *mode, *tenant, *single, *mapping, *dry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, mode, tenant string, single bool, mappingPath string, dry bool) error {
	if mode != "check" && mode != "bootstrap" && mode != "backfill" && mode != "verify" {
		return errors.New("unsupported maintenance mode")
	}
	cfg, err := config.NewFromEnvironment()
	if err != nil {
		return err
	}
	lock, err := agentstate.Lock(cfg.AgentDataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	provider, err := mysql.NewProvider(cfg)
	if err != nil {
		return err
	}
	db := provider.UseDB(ctx)
	if db == nil {
		return errors.New("MySQL persistence is required")
	}
	connection, err := db.DB()
	if err != nil {
		return err
	}
	defer connection.Close()
	transactions, err := mysql.NewTransactionManager(provider)
	if err != nil {
		return err
	}
	repository := agent.NewRepository(provider, transactions)
	bundles, err := agentbundle.New(cfg.AgentDataDir)
	if err != nil {
		return err
	}
	if mode != "bootstrap" {
		owners, err := readOwnership(mappingPath)
		if err != nil {
			return err
		}
		return migrate(ctx, db, repository, bundles, mode, tenant, single, owners, dry, os.Stdout)
	}
	if !identity.ValidID(tenant) {
		return errors.New("bootstrap requires an explicit tenant-id")
	}
	work := config.NewWorkDir(cfg)
	templates, err := agentprofile.NewCatalog(pi.WorkDir(work))
	if err != nil {
		return err
	}
	assembler, err := agenttemplate.New(string(work), templates)
	if err != nil {
		return err
	}
	journal, err := agentstate.New(cfg.AgentDataDir)
	if err != nil {
		return err
	}
	var collected struct {
		fx.In
		Tools []ai.Tool `group:"agent_tools"`
	}
	app := fx.New(chattools.Register, fx.Populate(&collected), fx.NopLogger)
	if err := app.Err(); err != nil {
		return err
	}
	names := []string{}
	available := map[string]bool{}
	for _, tool := range collected.Tools {
		name := tool.Definition().Name
		names = append(names, name)
		available[name] = true
	}
	smoke, err := agentsmoke.New(cfg.AgentTraining)
	if err != nil {
		return err
	}
	validator, err := agentversion.NewValidator(bundles, func(_ context.Context, s agentversion.Snapshot) error {
		if _, err := cfg.ResolveAgentModel(s.Model); err != nil {
			return err
		}
		for _, ref := range s.Tools.MCP {
			if _, err := cfg.ResolveAgentMCP(ref); err != nil {
				return err
			}
		}
		for _, ref := range s.Tools.Registered {
			if !available[ref.Name] || ref.ImplementationRef != "application:"+ref.Name+":v1" {
				return errors.New("application tool unavailable")
			}
		}
		return nil
	}, smoke.Check, func(context.Context) (time.Time, error) { return mysql.UTCNow(db) }, agentversion.ValidationPolicy{Version: "1", Environment: "offline-bootstrap-v1", Timeout: time.Duration(cfg.AgentTraining.SmokeTimeoutSeconds) * time.Second, TTL: time.Duration(cfg.AgentTraining.ValidationTTLSeconds) * time.Second})
	if err != nil {
		return err
	}
	builder, err := agentversion.NewInitialBuilder(bundles, journal, cfg, assembler, validator, names)
	if err != nil {
		return err
	}
	catalog, err := agentcatalog.NewService(repository, builder, templates, serviceimpl.NewIDService(int64(cfg.SnowflakeNodeID)), make([]byte, 32))
	if err != nil {
		return err
	}
	p := identity.Principal{TenantID: tenant, UserID: "operator", Role: identity.RoleAdmin}
	for _, template := range templates.List() {
		if dry {
			fmt.Fprintln(os.Stdout, "bootstrap", tenant, template.Code)
			continue
		}
		a, err := catalog.Bootstrap(ctx, p, template.Code)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "bootstrapped", a.ID, template.Code)
	}
	return nil
}
