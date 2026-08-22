package conversation

import "time"

type InvocationPhase string

const (
	InvocationPhaseThinking InvocationPhase = "thinking"
	InvocationPhaseAction   InvocationPhase = "action"
	// InvocationPhaseCompaction 是上下文摘要阶段（设计 §10.1：领域层补齐）。
	InvocationPhaseCompaction InvocationPhase = "compaction"
)

// InvocationOutcome 是可信调用的契约验收结果（§9.3）。
type InvocationOutcome string

const (
	InvocationOutcomeAccepted        InvocationOutcome = "accepted"
	InvocationOutcomeContractInvalid InvocationOutcome = "contract_invalid"
)

// ModelInvocation records token usage, price, cost, and latency for one model call.
//
// TraceID/ProviderRequestIndex/TTFTMS/FinishReason/ErrorCode 可空：仅在
// 阶段 3 起的新行上有值；Outcome/CostQuality 不允许依赖数据库默认值。
type ModelInvocation struct {
	ID                             string          `gorm:"column:id;primaryKey;size:32"`
	ConversationID                 string          `gorm:"column:conversation_id;size:32;not null;uniqueIndex:uq_agent_model_invocations_turn_sequence,priority:1"`
	TurnVersion                    uint64          `gorm:"column:turn_version;not null;uniqueIndex:uq_agent_model_invocations_turn_sequence,priority:2"`
	RunID                          string          `gorm:"column:run_id;size:128;not null;default:''"`
	Sequence                       uint32          `gorm:"column:sequence;not null;uniqueIndex:uq_agent_model_invocations_turn_sequence,priority:3"`
	Phase                          InvocationPhase `gorm:"column:phase;size:16;not null"`
	PlatformID                     string          `gorm:"column:platform_id;size:128;not null"`
	Model                          string          `gorm:"column:model;size:255;not null"`
	InputTokens                    int64           `gorm:"column:input_tokens;not null"`
	OutputTokens                   int64           `gorm:"column:output_tokens;not null"`
	InputPriceUSDPerMillionTokens  float64         `gorm:"column:input_price_usd_per_million_tokens;type:decimal(20,12);not null"`
	OutputPriceUSDPerMillionTokens float64         `gorm:"column:output_price_usd_per_million_tokens;type:decimal(20,12);not null"`
	CostUSD                        float64         `gorm:"column:cost_usd;type:decimal(20,12);not null"`
	LatencyMS                      int64           `gorm:"column:latency_ms;not null"`
	// TraceID 是当前 conversation.run SpanContext 的 32 位小写 TraceID；
	// SpanContext 无效（Telemetry 关闭）时为 NULL（§10.1）。
	TraceID *string `gorm:"column:trace_id;size:32"`
	// ProviderRequestIndex 是 Run 内物理 Provider 请求序号，用于在 Trace 中
	// 定位唯一 Provider Span；不保存 span_id。
	ProviderRequestIndex *uint32           `gorm:"column:provider_request_index"`
	Outcome              InvocationOutcome `gorm:"column:outcome;size:32;not null;default:accepted"`
	CostQuality          string            `gorm:"column:cost_quality;size:16;not null;default:estimated"`
	// 阶段 4（§10.1）：缓存/推理分项。口径：Cache Read/Write 是 Input 子集，
	// Reasoning 是 Output 子集；无符号列保证非负。
	CacheReadTokens                    int64   `gorm:"column:cache_read_tokens;not null;default:0"`
	CacheWriteTokens                   int64   `gorm:"column:cache_write_tokens;not null;default:0"`
	ReasoningTokens                    int64   `gorm:"column:reasoning_tokens;not null;default:0"`
	CacheReadPriceUSDPerMillionTokens  float64 `gorm:"column:cache_read_price_usd_per_million_tokens;type:decimal(20,12);not null;default:0"`
	CacheWritePriceUSDPerMillionTokens float64 `gorm:"column:cache_write_price_usd_per_million_tokens;type:decimal(20,12);not null;default:0"`
	// TTFTMS 可空：纯 Tool Call 为 NULL，已观测但不足 1ms 为 0（§10.1）。
	TTFTMS       *int64    `gorm:"column:ttft_ms"`
	FinishReason *string   `gorm:"column:finish_reason;size:32"`
	ErrorCode    *string   `gorm:"column:error_code;size:64"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (ModelInvocation) TableName() string { return "agent_model_invocations" }
