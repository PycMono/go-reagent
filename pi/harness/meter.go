package harness

import (
	"encoding/json"
	"math"

	"github.com/PycMono/go-reagent/pi/ai"
)

// 压缩策略的内部稳定默认值。未证明需要调参前不暴露为配置项；
// 取值与相互关系（PruneRatio < ThresholdRatio 等）由单元测试断言。
const (
	DefaultPruneRatio           = 0.70
	DefaultThresholdRatio       = 0.80
	DefaultReserveOutputTokens  = 4096
	DefaultSafetyMarginTokens   = 2048
	DefaultProtectRecentGroups  = 3
	DefaultMaxToolResultBytes   = 4096
	DefaultSummaryInputMaxBytes = 32 * 1024
	DefaultRetainRecentUnits    = 5

	// DefaultImageTokens 是单个 URL 图像块的固定 token 估算。无像素尺寸
	// 信息时按字节换算会严重高估，1024 为量级正确的近似；reactive overflow
	// 兜底仍然生效。
	DefaultImageTokens = 1024

	bytesPerTokenHeuristic = 4
)

// CompactionConfig 是外露的压缩配置；零值关闭主动压缩与 L1 pruner，
// reactive overflow 兜底始终启用。
type CompactionConfig struct {
	// ContextWindowTokens 是平台模型的上下文窗口容量；<= 0 关闭主动路径。
	ContextWindowTokens int64
	// EnablePrune 显式启用只读工具结果裁剪（L1）。
	EnablePrune bool
}

// RequestFootprint 是下一次主模型请求的模型可见内容。
type RequestFootprint struct {
	Messages []ai.Message
	Tools    ai.ToolDefinitions
}

// TokenMeter 用模型可见投影的序列化字节数近似请求的输入 token 数。
// 每次主请求前全量重算，不维护跨调用状态。
type TokenMeter struct{}

// Estimate 估算下一次请求的输入 token 数：len(投影 JSON)/4，
// 下界 clamp 到 0，长度累计使用饱和运算。该值只是近似，reactive 仍是最终兜底。
func (TokenMeter) Estimate(next RequestFootprint) int64 {
	total := VisibleMessagesBytes(next.Messages)
	total = saturatingAddInt(total, VisibleToolsBytes(next.Tools))

	return int64(total / bytesPerTokenHeuristic)
}

// visibleMessage 是 Provider 实际发送的消息投影：内部字段
// （Usage、FinishReason 等）不参与计量。
type visibleMessage struct {
	Role       ai.Role           `json:"role"`
	Text       string            `json:"text,omitempty"`
	ToolCalls  []visibleToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	IsError    bool              `json:"is_error,omitempty"`
}

type visibleToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// visibleTool 是 Provider 实际发送的工具 schema 投影：
// Label、ParallelSafe 等框架内部字段不参与计量。
type visibleTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

// VisibleMessagesBytes 返回消息列表模型可见投影的 JSON 序列化字节数。
// 摘要输入、压缩范围选择与 TokenMeter 计量使用同一投影口径。图像块额外
// 累加 DefaultImageTokens 折算的虚拟字节，使 Estimate、压缩范围选择与
// 收敛检查共用同一图像权重。
func VisibleMessagesBytes(messages []ai.Message) int {
	total := 0
	for _, message := range messages {
		projected := visibleMessage{
			Role:       message.Role,
			ToolCallID: message.ToolCallID,
			IsError:    message.IsError,
		}
		for _, block := range message.Content {
			switch block.Type {
			case ai.ContentTypeText:
				projected.Text += block.Text
			case ai.ContentTypeImage:
				if block.Image == nil {
					projected.Text += ai.ImagePlaceholderText("")
					total = saturatingAddInt(total, DefaultImageTokens*bytesPerTokenHeuristic)
					continue
				}
				projected.Text += ai.ImagePlaceholderText(block.Image.URL)
				total = saturatingAddInt(total, DefaultImageTokens*bytesPerTokenHeuristic)
			}
		}
		for _, call := range message.ToolCalls {
			projected.ToolCalls = append(projected.ToolCalls, visibleToolCall{
				ID: call.ID, Name: call.Name, Arguments: call.Arguments,
			})
		}
		total = saturatingAddInt(total, jsonLen(projected))
	}
	return total
}

// VisibleToolsBytes 返回工具定义模型可见投影的 JSON 序列化字节数。
func VisibleToolsBytes(definitions ai.ToolDefinitions) int {
	total := 0
	for _, definition := range definitions {
		total = saturatingAddInt(total, jsonLen(visibleTool{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.InputSchema,
		}))
	}
	return total
}

// MarshalVisibleMessages 返回消息列表模型可见投影的 JSON；
// 摘要输入使用与计量一致的投影口径，不包含 Usage 等内部字段。
// 图像块投影为脱敏占位文本，摘要"看得见"图像存在，但不含虚拟字节。
func MarshalVisibleMessages(messages []ai.Message) ([]byte, error) {
	projected := make([]visibleMessage, 0, len(messages))
	for _, message := range messages {
		next := visibleMessage{
			Role:       message.Role,
			ToolCallID: message.ToolCallID,
			IsError:    message.IsError,
		}
		for _, block := range message.Content {
			switch block.Type {
			case ai.ContentTypeText:
				next.Text += block.Text
			case ai.ContentTypeImage:
				imageURL := ""
				if block.Image != nil {
					imageURL = block.Image.URL
				}
				next.Text += ai.ImagePlaceholderText(imageURL)
			}
		}
		for _, call := range message.ToolCalls {
			next.ToolCalls = append(next.ToolCalls, visibleToolCall{
				ID: call.ID, Name: call.Name, Arguments: call.Arguments,
			})
		}
		projected = append(projected, next)
	}
	return json.Marshal(projected)
}

func jsonLen(value any) int {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return len(encoded)
}

func saturatingAddInt(a, b int) int {
	if b > 0 && a > math.MaxInt-b {
		return math.MaxInt
	}
	if b < 0 && a < math.MinInt-b {
		return math.MinInt
	}
	return a + b
}
