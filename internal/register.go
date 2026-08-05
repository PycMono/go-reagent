package internal

import (
	"context"
	"errors"
	"fmt"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/ai/providers"
	"github.com/PycMono/go-reagent/internal/app"
	"github.com/PycMono/go-reagent/internal/config"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/conversation"
	conversationmysql "github.com/PycMono/go-reagent/internal/conversation/mysql"
	"github.com/PycMono/go-reagent/internal/dispatch"
	drivermysql "github.com/PycMono/go-reagent/internal/driver/mysql"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/tools"
	"go.uber.org/fx"
)

// Register is the complete go-reagent dependency graph.
var Register = fx.Options(
	config.Register,
	drivermysql.Register,
	ctxpkg.Register,
	fx.Provide(newAIClient),
	tools.Register,
	dispatch.Register,
	engine.Register,
	conversationmysql.Register,
	conversation.Register,
	app.Register,
)

func newAIClient(cfg *config.Config) (ai.Client, error) {
	if cfg == nil {
		return nil, errors.New("初始化模型 Provider: 配置不能为空")
	}
	platform, err := cfg.Current()
	if err != nil {
		return nil, err
	}
	client, err := providers.New(platform)
	if err != nil {
		return nil, fmt.Errorf("初始化平台 %q: %w", platform.ID, err)
	}
	logsdk.Info(context.Background(), "模型平台初始化成功",
		logsdk.Any("component", "bootstrap"),
		logsdk.Any("platform_id", platform.ID),
		logsdk.Any("protocol", platform.Protocol),
		logsdk.Any("model", platform.Model),
	)
	return client, nil
}
