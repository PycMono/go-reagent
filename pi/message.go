package pi

import (
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/governor"
)

// RunRequest 保存一次无状态运行所需的调用方输入。
type RunRequest struct {
	// History 是本轮运行开始前、面向业务的文本会话历史。
	History []Message `json:"history,omitempty"`
	// Input 是本轮用户输入消息。
	Input Message `json:"input"`
	// Context 是本轮额外注入的业务上下文。
	Context []ContextBlock `json:"context,omitempty"`
	// Limits 是本轮运行的确定性资源上限；未配置（零值）的字段使用
	// governor.DefaultLimits 对应字段的默认值。
	Limits governor.Limits `json:"limits,omitempty"`
}

// Validate 校验请求的固有契约。它不触碰 History/Input 转换、Context
// 构造或任何 Provider 调用，调用方必须在那些步骤之前执行。
func (request RunRequest) Validate() error {
	if err := request.Limits.Validate(); err != nil {
		return err
	}
	for index, block := range request.Context {
		if strings.TrimSpace(block.Name) == "" {
			return fmt.Errorf("%w: context block %d name must not be empty", pierrors.ErrRequestInvalid, index)
		}
		if strings.TrimSpace(block.Content) == "" {
			return fmt.Errorf("%w: context block %d content must not be empty", pierrors.ErrRequestInvalid, index)
		}
	}

	return nil
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

// RunResult 保存一次运行中新产生的消息、模型调用记录和终止结果。
type RunResult struct {
	// NewMessages 是本次运行新增的 Assistant 和 Tool 消息。
	NewMessages []ai.Message `json:"new_messages,omitempty"`
	// Invocations 是本次运行已完成的模型调用记录：主运行的调用按完成顺序
	// 排列；子代理调用在其工具批次结算时按 drain 顺序追加，Sequence 单调
	// 递增，但 ProviderRequestIndex 不保证随账本顺序递增（跨子代理弱序）。
	Invocations []governor.Invocation `json:"invocations,omitempty"`
	// Termination 是本次运行的结构化终止结果。
	Termination governor.Termination `json:"termination"`
}
