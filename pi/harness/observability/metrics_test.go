package observability

import (
	"context"
	"math"
	"testing"
	"time"

	sdkmetrics "github.com/PycMono/go-observability-sdk/metrics"
	"github.com/PycMono/go-reagent/pi/ai"
)

// TestBucketsStrictlyIncreasing 保证所有显式 Bucket 有限且严格递增，

// 与设计 §8.5 的 View 固定要求一致。
func TestBucketsStrictlyIncreasing(t *testing.T) {
	buckets := map[string][]float64{
		"run_duration":      BucketsRunDuration,
		"provider_duration": BucketsProviderDuration,
		"ttft":              BucketsTTFT,
		"tool_duration":     BucketsToolDuration,
		"tool_queue":        BucketsToolQueue,
		"reduction_ratio":   BucketsReductionRatio,
		"turns":             BucketsTurns,
		"invocations":       BucketsInvocations,
		"genai_op_duration": BucketsGenAIOperationDuration,
		"genai_token_usage": BucketsGenAITokenUsage,
	}
	for name, bucket := range buckets {
		if len(bucket) == 0 {
			t.Fatalf("%s: 至少一个 bucket", name)
		}
		for index, value := range bucket {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatalf("%s: bucket %d 非有限值", name, index)
			}
			if index > 0 && value <= bucket[index-1] {
				t.Fatalf("%s: bucket 必须严格递增（%v）", name, bucket)
			}
		}
	}
}

// TestForbiddenLabelKeysCoverRedLine 校验 §8.5 基数红线字段全部被禁止：

// SDK 默认集合（metrics.DefaultForbiddenLabelKeys）∪ 项目增量。
func TestForbiddenLabelKeysCoverRedLine(t *testing.T) {
	// 设计红线 → 生效键（SDK 拼写）。
	redLine := []string{
		"run_id", "conversation_id", "trace_id", "span_id", "user_id",
		"gen_ai.tool.call.id", "session_id", "file.path", "command",
		"error.message", "prompt", "model_response",
	}
	forbidden := make(map[string]struct{})
	for _, key := range sdkmetrics.DefaultForbiddenLabelKeys() {
		forbidden[key] = struct{}{}
	}
	for _, key := range ForbiddenLabelKeys {
		forbidden[key] = struct{}{}
	}
	for _, key := range redLine {
		if _, ok := forbidden[key]; !ok {
			t.Fatalf("基数红线字段 %q 未被禁止（SDK 默认或项目增量）", key)
		}
	}
}

// TestDomainDefinitionsReferenceValidNames 校验领域指标命名满足 OTel

// Instrument 命名约束（长度、首字符、字符集），不引入 SDK 依赖。
func TestDomainDefinitionsReferenceValidNames(t *testing.T) {
	seen := make(map[string]struct{})
	for _, definition := range DomainMetricDefinitions() {
		name := definition.Name
		if name == "" || len(name) > 255 {
			t.Fatalf("非法指标名 %q", name)
		}
		for index := 0; index < len(name); index++ {
			c := name[index]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			case index > 0 && (c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-' || c == '/'):
			default:
				t.Fatalf("指标名 %q 第 %d 字节非法", name, index)
			}
		}
		if _, dup := seen[name]; dup {
			t.Fatalf("指标名 %q 重复", name)
		}
		seen[name] = struct{}{}
		if definition.Kind == sdkmetrics.KindHistogram && len(definition.Buckets) == 0 {
			t.Fatalf("Histogram %q 缺少 Bucket", name)
		}
		forbidden := make(map[string]struct{}, len(ForbiddenLabelKeys))
		for _, key := range ForbiddenLabelKeys {
			forbidden[key] = struct{}{}
		}
		for _, label := range definition.Labels {
			if _, bad := forbidden[label]; bad {
				t.Fatalf("指标 %q 使用禁止 Label %q", name, label)
			}
		}
	}
}

// ---------- Metrics（经 SDK 默认 Manager） ----------

type recordingAdaptor struct {
	entries []recordedMetric
}

type recordedMetric struct {
	kind   string
	name   string
	value  float64
	labels map[string]string
}

func labelsToMap(labels []sdkmetrics.Label) map[string]string {
	out := make(map[string]string, len(labels))
	for _, label := range labels {
		out[string(label.Key)] = label.Value.AsString()
	}
	return out
}

func (a *recordingAdaptor) Counter(_ context.Context, name string, value float64, labels ...sdkmetrics.Label) {
	a.entries = append(a.entries, recordedMetric{"counter", name, value, labelsToMap(labels)})
}
func (a *recordingAdaptor) UpDownCounter(_ context.Context, name string, value float64, labels ...sdkmetrics.Label) {
	a.entries = append(a.entries, recordedMetric{"updown", name, value, labelsToMap(labels)})
}
func (a *recordingAdaptor) Histogram(_ context.Context, name string, value float64, labels ...sdkmetrics.Label) {
	a.entries = append(a.entries, recordedMetric{"histogram", name, value, labelsToMap(labels)})
}
func (a *recordingAdaptor) Timer(_ context.Context, name string, seconds float64, labels ...sdkmetrics.Label) {
	a.entries = append(a.entries, recordedMetric{"timer", name, seconds, labelsToMap(labels)})
}
func (a *recordingAdaptor) Value(_ context.Context, name string, value float64, labels ...sdkmetrics.Label) {
	a.entries = append(a.entries, recordedMetric{"gauge", name, value, labelsToMap(labels)})
}

func installMetrics(t *testing.T) *recordingAdaptor {
	t.Helper()
	adaptor := &recordingAdaptor{}
	sdkmetrics.SetDefault(sdkmetrics.NewManager(adaptor))
	t.Cleanup(func() { sdkmetrics.SetDefault(nil) })
	return adaptor
}

func (a *recordingAdaptor) find(name string) *recordedMetric {
	for index := range a.entries {
		if a.entries[index].name == name {
			return &a.entries[index]
		}
	}
	return nil
}

func TestTracingProviderRecordsP0Metrics(t *testing.T) {
	installTracer(t)
	adaptor := installMetrics(t)
	stream := &fakeRawStream{
		events: []ai.StreamEvent{{Type: ai.StreamEventTextDelta, TextDelta: "好"}, {Type: ai.StreamEventDone}},
		result: meteredMessage("好"),
	}
	provider, _ := newTestChain(stream)

	s := provider.Stream(hintedContext(), nil, nil)
	for s.Next() {
	}
	s.Result()
	s.Close()

	requests := adaptor.find(MetricModelRequests)
	if requests == nil || requests.labels[LabelOutcome] != "success" ||
		requests.labels[LabelErrorCode] != ErrorCodeNone || requests.labels[LabelPhase] != "action" ||
		requests.labels[LabelProvider] != "test" || requests.labels[LabelModel] != "test-model" {
		t.Fatalf("requests 指标错误: %#v", requests)
	}
	if adaptor.find(MetricGenAIClientOperationDuration) == nil {
		t.Fatal("缺少 gen_ai.client.operation.duration")
	}
	if adaptor.find(MetricGenAIClientTokenUsage) == nil {
		t.Fatal("缺少 gen_ai.client.token.usage")
	}
	ttft := adaptor.find(MetricModelTTFT)
	if ttft == nil || ttft.labels[LabelPhase] != "action" {
		t.Fatalf("TTFT Histogram 缺失或 Label 错误: %#v", ttft)
	}
}

func TestNoopProviderPathDoesNotPanic(t *testing.T) {
	// 默认全局（未安装 Runtime）下整条链路安全空转。
	stream := &fakeRawStream{result: meteredMessage("x")}
	provider, _ := newTestChain(stream)
	s := provider.Stream(context.Background(), nil, nil)
	if _, err := s.Result(); err != nil {
		t.Fatal(err)
	}
	s.Close()
}

// TestP1MetricsRecorded 覆盖阶段 5 启用的 P1 指标（§8）。
func TestP1MetricsRecorded(t *testing.T) {
	adaptor := installMetrics(t)
	ctx := context.Background()

	RecordAgentRunShape(ctx, 3, 2)
	RecordChatRun(ctx, "default", string(TransportHTTPSSE), "completed")
	RecordCompactionDetail(ctx, CompactionReasonOverflow, nil, 2*time.Second, 10, 4)

	if got := adaptor.find(MetricAgentRunTurns); got == nil || got.value != 3 {
		t.Fatalf("run.turns = %#v", got)
	}
	if got := adaptor.find(MetricAgentRunInvocations); got == nil || got.value != 2 {
		t.Fatalf("run.invocations = %#v", got)
	}
	chat := adaptor.find(MetricChatRuns)
	if chat == nil || chat.labels[LabelProfile] != "default" ||
		chat.labels[LabelTransport] != "http_sse" || chat.labels[LabelTerminationReason] != "completed" {
		t.Fatalf("chat.runs = %#v", chat)
	}
	if got := adaptor.find(MetricCompactionDuration); got == nil || got.value != 2 {
		t.Fatalf("compaction.duration = %#v", got)
	}
	// 削减比例 = 1 - 4/10 = 0.6。
	if got := adaptor.find(MetricCompactionMessageReduction); got == nil || got.value != 0.6 {
		t.Fatalf("reduction ratio = %#v", got)
	}
}

// TestCompactionReductionRatioClamped 削减比例只在 before>0 时记录并限制在
// [0,1]（§8.4）。
func TestCompactionReductionRatioClamped(t *testing.T) {
	adaptor := installMetrics(t)
	// before=0：不记录。
	RecordCompactionDetail(context.Background(), CompactionReasonThreshold, nil, time.Second, 0, 0)
	if got := adaptor.find(MetricCompactionMessageReduction); got != nil {
		t.Fatalf("before=0 时不得记录: %#v", got)
	}
	// after>before：比例钳制到 0。
	RecordCompactionDetail(context.Background(), CompactionReasonThreshold, nil, time.Second, 5, 8)
	if got := adaptor.find(MetricCompactionMessageReduction); got == nil || got.value != 0 {
		t.Fatalf("越界必须钳制为 0: %#v", got)
	}
}

// TestRecordAPIShapeMatchesDefinitionKind 防止记录侧的 SDK API 形态
// （Timer/Histogram/Counter）与 Definition 的 Kind 漂移——SDK 的 Instrument
// 缓存首次创建即冻结类型，不一致的记录会被丢弃（kind conflict）。
func TestRecordAPIShapeMatchesDefinitionKind(t *testing.T) {
	adaptor := installMetrics(t)
	ctx := context.Background()

	RecordAgentRun(ctx, "completed", time.Second)
	RecordAgentRunShape(ctx, 2, 1)
	RecordChatRun(ctx, "default", "http_sse", "completed")
	RecordModelRequest(ctx, "test", "m", GenerationPhaseAction, nil)
	RecordModelInvocation(ctx, "test", "m", GenerationPhaseAction, AcceptanceAccepted, 0.1, CostQualityExact, 100, 50, 10, 5, 3)
	RecordModelRetry(ctx, "test", "m", GenerationPhaseAction, "ai_transient")
	RecordContextOverflow(ctx, "test", "m", GenerationPhaseAction)
	RecordGenAIClientOperation(ctx, "openai", "m", time.Second, nil)
	RecordGenAITokenUsage(ctx, "openai", "m", 100, 50)
	RecordModelTTFT(ctx, "test", "m", GenerationPhaseAction, 300*time.Millisecond)
	RecordToolExecution(ctx, "read", nil, time.Second)
	RecordToolQueueDuration(ctx, "read", ExecutionModeParallel, nil, time.Millisecond)
	RecordCompaction(ctx, CompactionReasonOverflow, nil)
	RecordCompactionDetail(ctx, CompactionReasonOverflow, nil, time.Second, 10, 4)

	kindByName := make(map[string]sdkmetrics.Kind)
	for _, definition := range DomainMetricDefinitions() {
		kindByName[definition.Name] = definition.Kind
	}
	// sdkmetrics API 形态 → 定义 Kind 的映射。
	apiKind := map[string]sdkmetrics.Kind{
		"counter": sdkmetrics.KindCounter, "timer": sdkmetrics.KindTimer, "histogram": sdkmetrics.KindHistogram,
	}
	if len(adaptor.entries) == 0 {
		t.Fatal("没有任何记录")
	}
	for _, entry := range adaptor.entries {
		want, ok := kindByName[entry.name]
		if !ok {
			t.Errorf("记录函数引用了未定义的指标 %q", entry.name)
			continue
		}
		if got := apiKind[entry.kind]; got != want {
			t.Errorf("指标 %q 记录 API 形态 %q 与定义 Kind %q 不一致", entry.name, got, want)
		}
	}
}
