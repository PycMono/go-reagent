package observability

// 本文件固定《Agent Tracing 与成本可观测性设计》（docs/superpowers/specs/
// 2026-08-20-agent-tracing-observability-design.md）第 4 章的 Trace 语义：
// Span 名称、属性 Key 与 Retry Event 名称。枚举见 enums.go，指标定义见
// metrics.go。
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
