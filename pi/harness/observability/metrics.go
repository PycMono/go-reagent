package observability

// 本文件固定设计 §8 的指标定义语义：指标名称、Label Key、Histogram Bucket、
// 领域 MetricDefinition 与 Label 基数红线。记录函数见 record.go。
//
// MetricDefinition 是 pi 私有的定义载体：pi 只依赖 OTel API 与
// go-observability-sdk/metrics 的记录 API，Definition 的注册转换由
// infrastructure/observability 负责（OBS-007 边界）。

// ---------- Metrics 名称与 Label Key（§8） ----------

const (
	MetricAgentRuns           = "reagent.agent.runs"
	MetricAgentRunDuration    = "reagent.agent.run.duration"
	MetricAgentRunTurns       = "reagent.agent.run.turns"
	MetricAgentRunInvocations = "reagent.agent.run.invocations"
	MetricChatRuns            = "reagent.chat.runs"

	MetricModelRequests         = "reagent.model.requests"
	MetricModelInvocations      = "reagent.model.invocations"
	MetricModelCost             = "reagent.model.cost"
	MetricModelTokens           = "reagent.model.tokens"
	MetricModelTTFT             = "reagent.model.ttft"
	MetricModelRetries          = "reagent.model.retries"
	MetricModelContextOverflows = "reagent.model.context_overflows"

	// OTel gen-ai 标准指标（§8.2，semconv genaiconv development）。
	MetricGenAIClientOperationDuration = "gen_ai.client.operation.duration"
	MetricGenAIClientTokenUsage        = "gen_ai.client.token.usage"

	MetricToolExecutions    = "reagent.tool.executions"
	MetricToolDuration      = "reagent.tool.duration"
	MetricToolQueueDuration = "reagent.tool.queue_duration"

	MetricCompactions                = "reagent.compactions"
	MetricCompactionDuration         = "reagent.compaction.duration"
	MetricCompactionMessageReduction = "reagent.compaction.message_reduction_ratio"
)

const (
	LabelAgent             = "agent"
	LabelTerminationReason = "termination_reason"
	LabelProfile           = "profile"
	LabelTransport         = "transport"
	LabelProvider          = "provider"
	LabelModel             = "model"
	LabelPhase             = "phase"
	LabelOutcome           = "outcome"
	LabelErrorCode         = "error_code"
	LabelAcceptance        = "acceptance"
	LabelCostQuality       = "cost_quality"
	LabelTokenType         = "token_type"
	LabelReason            = "reason"
	LabelTool              = "tool"
	LabelExecutionMode     = "execution_mode"
)

// ---------- Histogram Bucket（§8.5，由显式 View 固定） ----------

var (
	BucketsRunDuration      = []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}
	BucketsProviderDuration = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	BucketsTTFT             = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	BucketsToolDuration     = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	BucketsToolQueue        = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
	// 设计未单独列出 Compaction 时延 Bucket，复用 Provider 口径。
	BucketsCompactionDuration = BucketsProviderDuration
	BucketsReductionRatio     = []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95}
	BucketsTurns              = []float64{1, 2, 4, 8, 16, 32, 64}
	BucketsInvocations        = []float64{1, 2, 4, 8, 16, 32, 64}
	// gen_ai 标准指标 Bucket 来自 semconv 建议值。
	BucketsGenAIOperationDuration = []float64{0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92}
	BucketsGenAITokenUsage        = []float64{1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864}
)

// ---------- 领域 Metric Definition ----------

// MetricKind 是领域指标的 Instrument 类型。取值与
// go-observability-sdk/metrics.Kind 对齐；由 infrastructure/observability
// 负责转换注册。
type MetricKind string

const (
	MetricKindCounter   MetricKind = "counter"
	MetricKindHistogram MetricKind = "histogram"
	// MetricKindTimer 是经 sdkmetrics.Timer 记录的秒级时延指标；
	// 与 SDK 的 KindTimer 对齐，Unit 必须为 s。记录侧的 API 形态
	//（Timer vs Histogram）必须与定义的 Kind 一致，否则 Instrument
	// 冻结后冲突记录被丢弃。
	MetricKindTimer MetricKind = "timer"
)

// MetricDefinition 是领域指标的显式定义（§8），固定 Instrument 类型、
// Unit、允许的 Label Key 集合与 Histogram Bucket。
type MetricDefinition struct {
	Name        string
	Kind        MetricKind
	Unit        string
	Description string
	Labels      []string
	Buckets     []float64
	Priority    string // P0：阶段 0–3 上线；P1：先固定语义，阶段 5 启用记录
}

// DomainMetricDefinitions 返回 §8 全部领域指标定义；P0/P1 一并注册，
// P1 的记录从阶段 5 启用。
func DomainMetricDefinitions() []MetricDefinition {
	return []MetricDefinition{
		// Agent（§8.1）。
		{Name: MetricAgentRuns, Kind: MetricKindCounter, Unit: "{run}",
			Description: "Agent Run 总数", Labels: []string{LabelAgent, LabelTerminationReason}, Priority: "P0"},
		{Name: MetricAgentRunDuration, Kind: MetricKindTimer, Unit: "s",
			Description: "Agent Run 时延", Labels: []string{LabelAgent, LabelTerminationReason}, Buckets: BucketsRunDuration, Priority: "P0"},
		{Name: MetricAgentRunTurns, Kind: MetricKindHistogram, Unit: "1",
			Description: "每 Run Turn 数", Labels: []string{LabelAgent}, Buckets: BucketsTurns, Priority: "P1"},
		{Name: MetricAgentRunInvocations, Kind: MetricKindHistogram, Unit: "1",
			Description: "每 Run 模型调用数", Labels: []string{LabelAgent}, Buckets: BucketsInvocations, Priority: "P1"},
		{Name: MetricChatRuns, Kind: MetricKindCounter, Unit: "{run}",
			Description: "Chat 业务 Run 总数", Labels: []string{LabelProfile, LabelTransport, LabelTerminationReason}, Priority: "P1"},

		// Model（§8.2）。
		{Name: MetricModelRequests, Kind: MetricKindCounter, Unit: "{request}",
			Description: "物理模型请求数", Labels: []string{LabelProvider, LabelModel, LabelPhase, LabelOutcome, LabelErrorCode}, Priority: "P0"},
		{Name: MetricModelInvocations, Kind: MetricKindCounter, Unit: "{invocation}",
			Description: "可信 Usage 的模型调用数", Labels: []string{LabelProvider, LabelModel, LabelPhase, LabelAcceptance}, Priority: "P0"},
		{Name: MetricModelCost, Kind: MetricKindCounter, Unit: "USD",
			Description: "模型调用成本", Labels: []string{LabelProvider, LabelModel, LabelPhase, LabelCostQuality}, Priority: "P0"},
		{Name: MetricModelTokens, Kind: MetricKindCounter, Unit: "{token}",
			Description: "模型 Token 消耗", Labels: []string{LabelProvider, LabelModel, LabelPhase, LabelTokenType}, Priority: "P0"},
		{Name: MetricModelTTFT, Kind: MetricKindTimer, Unit: "s",
			Description: "首个非空 Text Delta 时延", Labels: []string{LabelProvider, LabelModel, LabelPhase}, Buckets: BucketsTTFT, Priority: "P1"},
		{Name: MetricModelRetries, Kind: MetricKindCounter, Unit: "{retry}",
			Description: "模型请求重试次数；reason 取 pierrors 稳定错误码", Labels: []string{LabelProvider, LabelModel, LabelPhase, LabelReason}, Priority: "P0"},
		{Name: MetricModelContextOverflows, Kind: MetricKindCounter, Unit: "{overflow}",
			Description: "Context Overflow 次数", Labels: []string{LabelProvider, LabelModel, LabelPhase}, Priority: "P0"},
		{Name: MetricGenAIClientOperationDuration, Kind: MetricKindTimer, Unit: "s",
			Description: "GenAI client 操作时延（semconv）",
			Labels:      []string{AttrGenAIOperationName, AttrGenAIProviderName, AttrGenAIRequestModel, AttrErrorType}, Buckets: BucketsGenAIOperationDuration, Priority: "P0"},
		{Name: MetricGenAIClientTokenUsage, Kind: MetricKindHistogram, Unit: "{token}",
			Description: "GenAI 单次请求 Token 用量（semconv）",
			Labels:      []string{AttrGenAIOperationName, AttrGenAIProviderName, AttrGenAIRequestModel, "gen_ai.token.type"}, Buckets: BucketsGenAITokenUsage, Priority: "P0"},

		// Tool（§8.3）。
		{Name: MetricToolExecutions, Kind: MetricKindCounter, Unit: "{execution}",
			Description: "Tool 执行次数", Labels: []string{LabelTool, LabelOutcome, LabelErrorCode}, Priority: "P0"},
		{Name: MetricToolDuration, Kind: MetricKindTimer, Unit: "s",
			Description: "Tool 执行时延", Labels: []string{LabelTool, LabelOutcome}, Buckets: BucketsToolDuration, Priority: "P0"},
		{Name: MetricToolQueueDuration, Kind: MetricKindTimer, Unit: "s",
			Description: "Tool 信号量排队时延", Labels: []string{LabelTool, LabelExecutionMode, LabelOutcome}, Buckets: BucketsToolQueue, Priority: "P1"},

		// Compaction（§8.4）。
		{Name: MetricCompactions, Kind: MetricKindCounter, Unit: "{compaction}",
			Description: "上下文压缩次数", Labels: []string{LabelReason, LabelOutcome}, Priority: "P0"},
		{Name: MetricCompactionDuration, Kind: MetricKindTimer, Unit: "s",
			Description: "上下文压缩时延", Labels: []string{LabelReason, LabelOutcome}, Buckets: BucketsCompactionDuration, Priority: "P1"},
		{Name: MetricCompactionMessageReduction, Kind: MetricKindHistogram, Unit: "1",
			Description: "压缩消息削减比例，取值 [0,1]", Labels: []string{LabelReason}, Buckets: BucketsReductionRatio, Priority: "P1"},
	}
}

// ---------- Label 基数红线（§8.5） ----------

// 设计 §8.5 的禁止字段中，以下已由 go-observability-sdk 的默认禁止集合
// 覆盖（见 metrics.DefaultForbiddenLabelKeys，拼写以 SDK 为准）：
//
//	run_id, conversation_id, trace_id, span_id, user_id, session_id,
//	gen_ai.tool.call.id, file.path（文件路径）, command（命令文本）,
//	prompt, error.message（错误正文）
//
// ForbiddenLabelKeys 只保留 SDK 未覆盖的项目增量，由 Runtime 经
// WithForbiddenLabelKeys 追加；默认集合不可解除。
var ForbiddenLabelKeys = []string{
	"model_response", // Model Response
}
