package dispatch

import (
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/engine"
	"go.uber.org/fx"
)

// Register provides the configured user-facing Agent reporter.
var Register = fx.Options(
	fx.Provide(NewReporter),
)

// NewReporter creates terminal output and optionally adds enterprise WeChat.
func NewReporter(cfg *config.Config) (engine.Reporter, error) {
	if cfg == nil {
		return nil, errors.New("初始化 Reporter: 配置不能为空")
	}
	terminalReporter := engine.NewTerminalReporter()
	if cfg.Bot.WeCom.WebhookURL == "" {
		return terminalReporter, nil
	}
	weComReporter, err := NewWeComReporter(cfg.Bot.WeCom.WebhookURL, nil)
	if err != nil {
		return nil, fmt.Errorf("初始化企业微信群 Reporter: %w", err)
	}
	return engine.NewMultiReporter([]engine.ReporterRegistration{
		{Name: "terminal", Order: 100, Reporter: terminalReporter},
		{Name: "wecom", Order: 200, Reporter: weComReporter},
	}), nil
}
