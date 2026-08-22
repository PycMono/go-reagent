package observability

import "github.com/PycMono/go-reagent/pi/ai"

// 本文件固定《Agent Tracing 与成本可观测性设计》（docs/superpowers/specs/
// 2026-08-20-agent-tracing-observability-design.md）第 4 章的 Trace 语义常量
// 与全部领域枚举：Span 名称、属性 Key、Retry Event 名称，以及 Phase/
// Outcome/Acceptance 等枚举（拼写由 semantics_test.go 锁定）。
// 指标名称/Label/Bucket 等 Metrics 语义见 metrics.go。
//
// gen_ai.* 名称封装自 OTel semantic-conventions gen-ai（development 状态），
// 精确 revision 见下；Development 名称统一在本文件封装，业务代码不得直接引用
// semconv 包或散落字符串字面量。
const GenAISemConvRevision = "go.opentelemetry.io/otel/semconv/v1.37.0 (genaiconv, development)"

// AgentName 是本项目唯一 Agent 的固定名称（§4.2：invoke_agent reagent）。
const AgentName = "reagent"

// ---------- Span 名称 ----------

const (
	SpanNameConversationRun         = "conversation.run"
	SpanNameConversationLoadHistory = "conversation.load_history"
	SpanNameConversationPersistTurn = "conversation.persist_turn"
	SpanNamePrepareContext          = "prepare_context"
	SpanNameTurn                    = "reagent.turn"
	SpanNameGenerate                = "reagent.generate"
	SpanNameCompaction              = "reagent.compact_context"
)

// AgentSpanName 返回 `invoke_agent {agent}`（§4.2）。
func AgentSpanName(agent string) string { return "invoke_agent " + agent }

// ChatSpanName 返回 `chat {model}`（§4.5）。
func ChatSpanName(model string) string { return "chat " + model }

// ToolSpanName 返回 `execute_tool {tool}`（§4.6）。
func ToolSpanName(tool string) string { return "execute_tool " + tool }

// ---------- Span 属性 Key ----------

const (
	// gen_ai 标准属性（semconv gen-ai development，统一封装）。
	AttrGenAIOperationName     = "gen_ai.operation.name"
	AttrGenAIAgentName         = "gen_ai.agent.name"
	AttrGenAIAgentVersion      = "gen_ai.agent.version"
	AttrGenAIConversationID    = "gen_ai.conversation.id"
	AttrGenAIProviderName      = "gen_ai.provider.name"
	AttrGenAIRequestModel      = "gen_ai.request.model"
	AttrGenAIResponseModel     = "gen_ai.response.model"
	AttrGenAIResponseFinishRsn = "gen_ai.response.finish_reasons"
	AttrGenAIUsageInputTokens  = "gen_ai.usage.input_tokens"
	AttrGenAIUsageOutputTokens = "gen_ai.usage.output_tokens"
	AttrGenAIToolName          = "gen_ai.tool.name"
	AttrGenAIToolCallID        = "gen_ai.tool.call.id"
	AttrErrorType              = "error.type"
	AttrReagentErrorCode       = "reagent.error.code"

	// conversation.run（§4.2）。
	AttrRunID              = "reagent.run.id"
	AttrProfileCode        = "reagent.profile.code"
	AttrRunTransport       = "reagent.run.transport"
	AttrPersistenceEnabled = "reagent.persistence.enabled"

	// invoke_agent（§4.2）。
	AttrTerminationReason = "reagent.termination.reason"
	AttrRunTurns          = "reagent.run.turns"
	AttrRunInvocations    = "reagent.run.invocations"
	AttrRunTotalTokens    = "reagent.run.total_tokens"
	AttrRunCostUSD        = "reagent.run.cost_usd"

	// reagent.turn（§4.3）。
	AttrTurnIndex             = "reagent.turn.index"
	AttrContextMessageCount   = "reagent.context.message_count"
	AttrContextEstimatedToken = "reagent.context.estimated_tokens"
	AttrToolsAvailableCount   = "reagent.tools.available_count"
	AttrToolsRequestedCount   = "reagent.tools.requested_count"
	AttrToolsExecutionMode    = "reagent.tools.execution_mode"

	// reagent.generate（§4.4）。
	AttrGenerationPhase     = "reagent.generation.phase"
	AttrGenerationAttempts  = "reagent.generation.attempts"
	AttrGenerationOutcome   = "reagent.generation.outcome"
	AttrCompactionTriggered = "reagent.compaction.triggered"

	// chat {model}（§4.5）。
	AttrUsageCacheReadTokens  = "reagent.usage.cache_read_tokens"
	AttrUsageCacheWriteTokens = "reagent.usage.cache_write_tokens"
	AttrUsageReasoningTokens  = "reagent.usage.reasoning_tokens"
	AttrProviderAttempt       = "reagent.provider.attempt"
	AttrStreamChunkCount      = "reagent.stream.chunk_count"
	AttrStreamTTFTMS          = "reagent.stream.ttft_ms"
	AttrInvocationCostUSD     = "reagent.invocation.cost_usd"
	AttrProviderRequestIndex  = "reagent.provider.request_index"

	// execute_tool（§4.6）。
	AttrToolParallelSafe  = "reagent.tool.parallel_safe"
	AttrToolIsError       = "reagent.tool.is_error"
	AttrToolArgumentsSize = "reagent.tool.arguments_size"
	AttrToolOutputSize    = "reagent.tool.output_size"

	// reagent.compact_context（§4.7）。
	AttrCompactionReason             = "reagent.compaction.reason"
	AttrCompactionBeforeMessageCount = "reagent.compaction.before_message_count"
	AttrCompactionAfterMessageCount  = "reagent.compaction.after_message_count"
	AttrCompactionBeforeTokens       = "reagent.compaction.before_tokens"
	AttrCompactionAfterTokens        = "reagent.compaction.after_tokens"
	AttrCompactionSummaryTokens      = "reagent.compaction.summary_tokens"
)

// ---------- Retry Wait Event（§4.8） ----------

const (
	EventRetryScheduled = "reagent.retry.scheduled"
	EventRetryCompleted = "reagent.retry.completed"
	EventRetryCanceled  = "reagent.retry.canceled"

	AttrRetryNextAttempt   = "reagent.retry.next_attempt"
	AttrRetryDelayMS       = "reagent.retry.delay_ms"
	AttrRetryActualDelayMS = "reagent.retry.actual_delay_ms"
	AttrRetryReason        = "reagent.retry.reason"
	AttrRetryCancelReason  = "reagent.retry.cancel_reason"
)

// ---------- 领域枚举 ----------

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
