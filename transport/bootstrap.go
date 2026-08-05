package transport

import (
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi/agent"
	"go.uber.org/fx"
)

// Module provides terminal and optional WeCom reporting.
var Module = fx.Options(
	fx.Provide(
		fx.Annotate(
			newTerminalReporterRegistration,
			fx.ResultTags(`group:"reporters"`),
		),
		fx.Annotate(
			NewReporterRegistrations,
			fx.ResultTags(`group:"reporters,flatten"`),
		),
		newRegisteredReporter,
	),
)

type reporterParams struct {
	fx.In

	Registrations []agent.ReporterRegistration `group:"reporters"`
}

func newTerminalReporterRegistration() agent.ReporterRegistration {
	return agent.ReporterRegistration{Name: "terminal", Order: 100, Reporter: NewTerminalReporter()}
}

func newRegisteredReporter(params reporterParams) agent.Reporter {
	return agent.NewMultiReporter(params.Registrations)
}

// NewReporterRegistrations creates the optional enterprise WeChat registration.
func NewReporterRegistrations(cfg *config.Config) ([]agent.ReporterRegistration, error) {
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
