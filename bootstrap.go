package reagent

import (
	"context"
	"errors"
	"os"

	agentcore "github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/internal/bootstrap"
	"github.com/PycMono/go-reagent/internal/workspace"
	"go.uber.org/fx"
)

func cloneConfig(input *Config) *Config {
	if input == nil {
		return nil
	}
	cloned := *input
	cloned.Platforms = append([]PlatformConfig(nil), input.Platforms...)
	for index := range cloned.Platforms {
		if input.Platforms[index].Pricing != nil {
			pricing := *input.Platforms[index].Pricing
			cloned.Platforms[index].Pricing = &pricing
		}
	}
	return &cloned
}

func buildAgent(config *Config) (*fx.App, agentcore.Runner, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	platform, err := config.Current()
	if err != nil {
		return nil, nil, err
	}

	var runtime *agentcore.Agent
	app := fx.New(
		fx.NopLogger,
		fx.Supply(platform, workspace.WorkDir(workDir)),
		bootstrap.Module,
		fx.Populate(&runtime),
	)
	if err := app.Err(); err != nil {
		return nil, nil, err
	}
	if err := app.Start(context.Background()); err != nil {
		stopErr := app.Stop(context.Background())
		return nil, nil, errors.Join(err, stopErr)
	}
	return app, runtime, nil
}
