// Package notice 装配外部通知通道（企业微信、飞书等）：配置启用哪个通道
// 就把哪个注册进 agent_notifiers 组，由 pi 聚合并桥接 run 的最终回复。
package notice

import (
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/infrastructure/notice/wecom"
	"github.com/PycMono/go-reagent/pi"
	"go.uber.org/fx"
)

var Register = fx.Options(
	fx.Provide(newNotifiers),
)

type notifiersOut struct {
	fx.Out
	Notifiers []pi.Notifier `group:"agent_notifiers,flatten"`
}

func newNotifiers(cfg *config.Config) notifiersOut {
	var notifiers []pi.Notifier
	if cfg.Notice.WeCom.WebhookURL != "" {
		notifiers = append(notifiers, wecom.New(cfg.Notice.WeCom.WebhookURL, nil))
	}
	return notifiersOut{Notifiers: notifiers}
}
