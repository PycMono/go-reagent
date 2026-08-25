package pi

import (
	"context"
	"fmt"
)

// NotificationKind 是告警类别。
type NotificationKind string

const (
	// NotificationRunError 是 run 以错误终止（Provider 失败、内部错误、
	// 上下文 deadline 等；用户主动取消不算）。
	NotificationRunError NotificationKind = "run_error"
	// NotificationRunLimit 是 run 触发请求级预算上限（轮次/成本/Token）。
	NotificationRunLimit NotificationKind = "run_limit"
	// NotificationToolError 是单次工具执行失败（toolexec.Result.IsError）。
	NotificationToolError NotificationKind = "tool_error"
)

// Notification 是一条运行告警。Summary 是面向告警通道的一句话描述，
// 由 pi 生成并保证脱敏（不含消息正文、工具参数、密钥）。
type Notification struct {
	Kind    NotificationKind
	Summary string
}

// Notifier 接收 Agent 运行告警，实现方是企业微信、飞书等外部通知通道。
//
// Notify 在 loop/Run 收尾路径上被同步串行调用：实现必须快速返回或自行
// 异步化。pi 对每个 Notifier 做 panic 兜底，不重试，告警失败不影响 run。
// 正常回复不产生任何通知。
type Notifier interface {
	Notify(ctx context.Context, notification Notification)
}

// alertListener 监听事件流，把工具失败翻译成 Notifier 告警。告警时机与
// 脱敏语义由 pi 统一定义，通道实现方不需要理解事件模型。
type alertListener struct {
	notifiers []Notifier
}

func (b *alertListener) OnEvent(ctx context.Context, event AgentEvent) {
	if event.Type != AgentEventToolEnd || event.Tool == nil || event.Tool.Result == nil {
		return
	}
	result := event.Tool.Result
	if !result.IsError {
		return
	}
	summary := fmt.Sprintf("工具 %s 执行失败", result.ToolName)
	if result.ErrorCode != "" {
		summary += fmt.Sprintf("（错误码 %s）", result.ErrorCode)
	}
	b.notify(ctx, Notification{Kind: NotificationToolError, Summary: summary})
}

func (b *alertListener) notify(ctx context.Context, notification Notification) {
	for _, notifier := range b.notifiers {
		notifySafely(ctx, notifier, notification)
	}
}

func notifySafely(ctx context.Context, notifier Notifier, notification Notification) {
	defer func() {
		_ = recover()
	}()
	notifier.Notify(ctx, notification)
}
