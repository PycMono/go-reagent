package provider

import (
	"context"
	"errors"
	"fmt"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/config"
	"go.uber.org/fx"
)

// Register provides the configured LLM platform adapter.
var Register = fx.Options(
	fx.Provide(NewLLMProvider),
)

// NewLLMProvider creates the currently selected model provider.
func NewLLMProvider(cfg *config.Config) (LLMProvider, error) {
	if cfg == nil {
		return nil, errors.New("初始化模型 Provider: 配置不能为空")
	}
	platform, err := cfg.Current()
	if err != nil {
		return nil, err
	}
	llmProvider, err := New(Options{
		Name:     platform.ID,
		Protocol: platform.Protocol,
		BaseURL:  platform.BaseURL,
		APIKey:   platform.APIKey,
		Model:    platform.Model,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化平台 %q: %w", platform.ID, err)
	}
	logsdk.Info(context.Background(), "模型平台初始化成功",
		logsdk.Any("component", "bootstrap"),
		logsdk.Any("platform_id", platform.ID),
		logsdk.Any("protocol", platform.Protocol),
		logsdk.Any("model", platform.Model),
	)
	return llmProvider, nil
}
