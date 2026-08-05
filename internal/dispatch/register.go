package dispatch

import (
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent"
	"github.com/PycMono/go-reagent/agent"
	"go.uber.org/fx"
)

// Register contributes optional dispatch reporters to the shared reporter group.
var Register = fx.Options(
	fx.Provide(
		fx.Annotate(
			NewReporterRegistrations,
			fx.ResultTags(`group:"reporters,flatten"`),
		),
	),
)

// NewReporterRegistrations creates the optional enterprise WeChat registration.
func NewReporterRegistrations(cfg *reagent.Config) ([]agent.ReporterRegistration, error) {
	if cfg == nil {
		return nil, errors.New("初始化 Reporter: 配置不能为空")
	}
	if cfg.Bot.WeCom.WebhookURL == "" {
		return nil, nil
	}
	weComReporter, err := NewWeComReporter(cfg.Bot.WeCom.WebhookURL, nil)
	if err != nil {
		return nil, fmt.Errorf("初始化企业微信群 Reporter: %w", err)
	}

	return []agent.ReporterRegistration{{Name: "wecom", Order: 200, Reporter: weComReporter}}, nil
}
