package pi

import (
	"context"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// runState 是一次 Run 的全部可变状态。
type runState struct {
	newMessages    []ai.Message
	invocations    []governor.Invocation
	contextHistory []ai.Message
	availableTools ai.ToolDefinitions
	callSequence   uint32
	// pendingReminder 是循环护栏排入下一次 Action 的 ephemeral 提醒：只进入
	// 下一次逻辑生成的 Provider 请求（含 retry 与 overflow 恢复），不进入
	// contextHistory、newMessages、事件流或压缩摘要；消费一次即清空。
	pendingReminder []ai.Message
}

// addInvocation 追加一条可信 Invocation，并返回其在
// state.invocations 中的下标。Outcome 初始为 accepted，由
// 调用方在契约校验后固定。
func (state *runState) addInvocation(
	phase governor.InvocationPhase,
	usage ai.Usage,
	requestIndex uint32,
	finishReason string,
) int {
	if usage.CostQuality == "" {
		usage.CostQuality = ai.CostQualityEstimated
	}
	state.callSequence++
	state.invocations = append(state.invocations, governor.Invocation{
		Sequence:             state.callSequence,
		Phase:                phase,
		Usage:                usage,
		Outcome:              governor.OutcomeAccepted,
		ProviderRequestIndex: requestIndex,
		FinishReason:         finishReason,
	})
	return len(state.invocations) - 1
}

// appendAssistantMessage 提交一条完整 Assistant 消息：写入 contextHistory 与
// newMessages，并补发 message_end 事件。Run 内所有提交路径共用。
func (state *runState) appendAssistantMessage(ctx context.Context, listener EventListener, msg *ai.Message) {
	state.contextHistory = append(state.contextHistory, *msg)
	state.newMessages = append(state.newMessages, *msg)
	EmitMessageEnd(ctx, listener, *msg)
}

// appendToolResultMessage 将工具结果写入模型历史和本次运行的新消息。
func (state *runState) appendToolResultMessage(event toolexec.Event) {
	message := event.ResultMessage()
	state.contextHistory = append(state.contextHistory, message)
	state.newMessages = append(state.newMessages, message)
}
