package dispatch

import (
	"fmt"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/engine"
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
func NewReporterRegistrations(cfg *config.Config) ([]engine.ReporterRegistration, error) {
	weComReporter, err := NewWeComReporter(cfg.Bot.WeCom.WebhookURL, nil)
	if err != nil {
		return nil, fmt.Errorf("初始化企业微信群 Reporter: %w", err)
	}

	return []engine.ReporterRegistration{{Name: "wecom", Order: 200, Reporter: weComReporter}}, nil
}
