package pi

import (
	"context"
	"fmt"
	"strings"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/PycMono/go-reagent/pi/loopdetect"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// commitLoopRecoveryResults 为被循环护栏 recover 阻止的批次提交完整协议组：
// 每个 Tool Call（含被排除工具——整批没有任何调用启动）按原始顺序补发
// synthetic start/end 事件，并写入与之一一对应的合成结果。
func commitLoopRecoveryResults(
	ctx context.Context,
	state *runState,
	calls ai.ToolCalls,
	listener EventListener,
) {
	for _, call := range calls {
		result := toolexec.NewRejectedEvent(call, pierrors.ErrorCodeRunLoopDetected,
			"工具循环护栏阻止了本批次执行：检测到重复且无进展的调用。请停止当前重试路径，改用不同方案，或明确说明无法继续。")
		EmitToolEvent(ctx, listener, toolexec.NewStartEvent(call))
		EmitToolEvent(ctx, listener, result)
		state.appendToolResultMessage(result)
	}
}

// newLoopReminderMessage 构造 warn 决策的 ephemeral 提醒：一条 Role=system
// 消息，只包含 Pattern、次数和工具名，不含参数、结果或消息正文。
func newLoopReminderMessage(intervention *loopdetect.Intervention) ai.Message {
	text := fmt.Sprintf(
		"循环护栏提醒：检测到重复的工具调用（模式 %s，第 %d 次，工具 %s）。"+
			"请检查之前的工具结果是否有变化；如果没有进展，请停止重试、改用其他方法，或明确说明无法继续。",
		intervention.Pattern, intervention.Count, strings.Join(intervention.ToolNames, ", "))
	return ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock(text)}}
}

// recordLoopIntervention 记录一次护栏干预的无正文结构化观测：日志字段、
// Turn Span 属性；禁止记录参数、结果或 hash。
func recordLoopIntervention(ctx context.Context, intervention *loopdetect.Intervention) {
	logsdk.Warn(ctx, "[Engine] 工具循环护栏干预",
		logsdk.Any("component", "engine"),
		logsdk.Any("pattern", string(intervention.Pattern)),
		logsdk.Any("level", string(intervention.Level)),
		logsdk.Any("count", intervention.Count),
		logsdk.Any("tool_names", intervention.ToolNames),
	)

	contexttracing.WithKV(ctx,
		contexttracing.KV(observability.AttrLoopDetectionPattern, string(intervention.Pattern)),
		contexttracing.KV(observability.AttrLoopDetectionLevel, string(intervention.Level)),
		contexttracing.KV(observability.AttrLoopDetectionCount, intervention.Count),
	)

}
