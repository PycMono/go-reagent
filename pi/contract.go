package pi

import "github.com/PycMono/go-reagent/pi/ai"

// RunRequest 保存一次无状态运行所需的调用方输入。
type RunRequest struct {
	// History 是本轮运行开始前的会话历史。
	History []HistoryMessage `json:"history,omitempty"`
	// Input 是本轮用户输入文本。
	Input string `json:"input"`
	// Context 是本轮额外注入的业务上下文。
	Context []ContextBlock `json:"context,omitempty"`
}

// ContextBlock 表示运行时注入到会话历史之前的一段业务上下文。
type ContextBlock struct {
	// Name 是上下文名称。
	Name string `json:"name"`
	// Content 是上下文内容。
	Content string `json:"content"`
	// Priority 决定上下文的排列顺序，数值越大越靠前。
	Priority int `json:"priority,omitempty"`
}

// ModelInvocationPhase 表示 Agent 调用模型的阶段。
type ModelInvocationPhase string

const (
	// ModelInvocationPhaseThinking 表示内部思考阶段的模型调用。
	ModelInvocationPhaseThinking ModelInvocationPhase = "thinking"
	// ModelInvocationPhaseAction 表示生成回复或工具调用的模型调用。
	ModelInvocationPhaseAction ModelInvocationPhase = "action"
)

// ModelInvocation 记录一次已完成且已计量的模型调用。
type ModelInvocation struct {
	// Sequence 是本次运行内从 1 开始的模型调用顺序。
	Sequence uint32 `json:"sequence"`
	// Phase 是本次模型调用所处的阶段。
	Phase ModelInvocationPhase `json:"phase"`
	// Usage 是本次模型调用的令牌、成本和耗时信息。
	Usage ai.Usage `json:"usage"`
}

// RunResult 保存一次运行中新产生的消息和模型调用记录。
type RunResult struct {
	// NewMessages 是本次运行新增的 Assistant 和 Tool 消息。
	NewMessages []ai.Message `json:"new_messages,omitempty"`
	// Invocations 是本次运行按顺序完成的模型调用记录。
	Invocations []ModelInvocation `json:"invocations,omitempty"`
}
