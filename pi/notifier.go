package pi

import (
	"context"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

// Notification 是一次 run 值得通知外部通道的结果快照。
type Notification struct {
	Text string // 最终 Assistant 回复正文
}

// Notifier 接收 run 级通知，实现方是企业微信、飞书等外部通知通道。
//
// Notify 在 loop 收尾路径上被同步串行调用：实现必须快速返回或自行
// 异步化。pi 对每个 Notifier 做 panic 兜底，不重试，通知失败不影响 run。
type Notifier interface {
	Notify(ctx context.Context, notification Notification)
}

// notifyBridge 把事件流翻译成 Notifier 回调：只认"无工具调用的最终
// Assistant 消息"，空文本不通知。通知时机、过滤语义由 pi 统一定义，
// 通道实现方不需要理解事件模型。
type notifyBridge struct {
	notifiers []Notifier
}

func (b *notifyBridge) Report(ctx context.Context, event AgentEvent) {
	if event.Type != AgentEventMessageEnd || event.Message == nil {
		return
	}
	if event.Message.Role != ai.RoleAssistant || len(event.Message.ToolCalls) != 0 {
		return
	}
	text, err := ai.TextContent(event.Message.Content)
	if err != nil || strings.TrimSpace(text) == "" {
		return
	}
	for _, notifier := range b.notifiers {
		notifySafely(ctx, notifier, Notification{Text: text})
	}
}

func notifySafely(ctx context.Context, notifier Notifier, notification Notification) {
	defer func() {
		_ = recover()
	}()
	notifier.Notify(ctx, notification)
}
