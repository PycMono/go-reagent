package observability

// 本文件固定设计（§4、§8、§9）的全部领域枚举；拼写由 attributes_test.go
// 锁定，防止实现期漂移。

import "github.com/PycMono/go-reagent/pi/ai"

// GenerationPhase 是逻辑生成或物理请求所属阶段（§4.4、§4.5、§7）。
type GenerationPhase string

const (
	GenerationPhaseThinking   GenerationPhase = "thinking"
	GenerationPhaseAction     GenerationPhase = "action"
	GenerationPhaseCompaction GenerationPhase = "compaction"
	// GenerationPhaseUnknown 是 GenerationHint 缺失时的兜底（§7）。
	GenerationPhaseUnknown GenerationPhase = "unknown"
)

// GenerationOutcome 是逻辑 Generate Span 的最终结果（§4.4）。
type GenerationOutcome string

const (
	GenerationOutcomeSucceeded        GenerationOutcome = "succeeded"
	GenerationOutcomeFailed           GenerationOutcome = "failed"
	GenerationOutcomeCanceled         GenerationOutcome = "canceled"
	GenerationOutcomeDeadlineExceeded GenerationOutcome = "deadline_exceeded"
)

// RequestOutcome 是物理请求/Tool/Compaction 指标的 outcome Label（§8.2–8.4）。
// 无错误时 error_code 统一填 ErrorCodeNone。
type RequestOutcome string

const (
	RequestOutcomeSuccess          RequestOutcome = "success"
	RequestOutcomeError            RequestOutcome = "error"
	RequestOutcomeCanceled         RequestOutcome = "canceled"
	RequestOutcomeDeadlineExceeded RequestOutcome = "deadline_exceeded"
)

// ErrorCodeNone 是 Metrics error_code Label 在无错误时的固定填充值。
const ErrorCodeNone = "none"

// Acceptance 是可信 Invocation 的契约验收结果（§8.2、§9.3）。
type Acceptance string

const (
	AcceptanceAccepted        Acceptance = "accepted"
	AcceptanceContractInvalid Acceptance = "contract_invalid"
)

// CostQuality 是成本可信度（§8.2、§9.1）；取值定义在 ai.Usage 所属包，
// 此处为类型别名以保持 Metrics 代码的统一引用。
type CostQuality = ai.CostQuality

const (
	CostQualityExact     = ai.CostQualityExact
	CostQualityEstimated = ai.CostQualityEstimated
)

// TokenType 是 reagent.model.tokens 的 token_type Label（§8.2）；
// 后三项是子集，不能全部求和。
type TokenType string

const (
	TokenTypeInputTotal  TokenType = "input_total"
	TokenTypeOutputTotal TokenType = "output_total"
	TokenTypeCacheRead   TokenType = "cache_read"
	TokenTypeCacheWrite  TokenType = "cache_write"
	TokenTypeReasoning   TokenType = "reasoning"
)

// CompactionReason 是 Compaction 触发原因（§4.7）；manual 为保留枚举。
type CompactionReason string

const (
	CompactionReasonOverflow  CompactionReason = "overflow"
	CompactionReasonThreshold CompactionReason = "threshold"
	CompactionReasonManual    CompactionReason = "manual"
)

// ExecutionMode 是 Turn 内 Tool 调度模式（§4.3、§8.3）。
type ExecutionMode string

const (
	ExecutionModeSerial   ExecutionMode = "serial"
	ExecutionModeParallel ExecutionMode = "parallel"
	ExecutionModeMixed    ExecutionMode = "mixed"
)

// Transport 是业务 Run 的入口通道（§4.2）。
type Transport string

const (
	TransportHTTPSSE  Transport = "http_sse"
	TransportTerminal Transport = "terminal"
	TransportWeCom    Transport = "wecom"
	TransportSDK      Transport = "sdk"
)

// RetryCancelReason 是 Retry 等待被取消的原因（§4.8）。
type RetryCancelReason string

const (
	RetryCancelContextCanceled  RetryCancelReason = "context_canceled"
	RetryCancelDeadlineExceeded RetryCancelReason = "deadline_exceeded"
)

// ---------- 内容策略（§11） ----------

// ContentModeNone 是唯一合法的内容采集模式：只记录元数据、长度、状态和
// Token，不采集可还原的模型或 Tool 正文。其他值在配置校验期启动失败。
const ContentModeNone = "none"
