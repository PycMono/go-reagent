package observability

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	sdkmetrics "github.com/PycMono/go-observability-sdk/metrics"
	piobservability "github.com/PycMono/go-reagent/pi/harness/observability"
	"gopkg.in/yaml.v3"
)

// opsSeriesPrefixes 把 Definition 正向翻译为 Prometheus 序列名前缀
// （UnderscoreEscapingWithSuffixes：点转下划线；单位 s→seconds、USD 保留、
// {…} 注解丢弃；Counter 追加 _total；Histogram 数据点追加 _bucket/_count/_sum）。
func opsSeriesPrefixes(definitions []sdkmetrics.Definition) []string {
	unitSuffix := func(unit string) string {
		switch {
		case unit == "s":
			return "_seconds"
		case unit == "USD":
			return "_USD"
		case strings.HasPrefix(unit, "{"), unit == "1", unit == "":
			return ""
		default:
			return "_" + unit
		}
	}
	prefixes := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		base := strings.ReplaceAll(definition.Name, ".", "_") + unitSuffix(definition.Unit)
		if definition.Kind == sdkmetrics.KindCounter {
			base += "_total"
		}
		prefixes = append(prefixes, base)
	}
	return prefixes
}

var opsMetricNamePattern = regexp.MustCompile(`\b(reagent|gen_ai)_[a-z0-9_]+\b`)

// knownOpsSeries 是 Definition 之外的合法引用：Collector 自身指标。
var knownOpsSeries = map[string]bool{
	"otelcol_receiver_refused_spans_total": true,
}

// TestOpsArtifactsReferenceKnownMetrics 校验告警规则与 Dashboard 只引用已
// 定义的 reagent/gen_ai 指标（防止部署件与代码漂移）。
func TestOpsArtifactsReferenceKnownMetrics(t *testing.T) {
	prefixes := opsSeriesPrefixes(piobservability.DomainMetricDefinitions())

	files := []string{
		"../../../deploy/observability/prometheus-rules.yaml",
		"../../../deploy/observability/grafana/provisioning/dashboards/reagent-agent.json",
		"../../../deploy/observability/grafana/provisioning/dashboards/reagent-model.json",
		"../../../deploy/observability/grafana/provisioning/dashboards/reagent-tool.json",
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, series := range opsMetricNamePattern.FindAllString(string(content), -1) {
			if knownOpsSeries[series] {
				continue
			}
			matched := false
			for _, prefix := range prefixes {
				if series == prefix || strings.HasPrefix(series, prefix+"_") {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("%s 引用了未知指标 %q", file, series)
			}
		}
	}
}

// TestOpsArtifactsParse 校验部署件语法有效。
func TestOpsArtifactsParse(t *testing.T) {
	for _, file := range []string{
		"../../../deploy/observability/docker-compose.yaml",
		"../../../deploy/observability/otel-collector.yaml",
		"../../../deploy/observability/otel-collector-tail.yaml",
		"../../../deploy/observability/prometheus.yml",
		"../../../deploy/observability/prometheus-rules.yaml",
		"../../../deploy/observability/tempo.yaml",
		"../../../deploy/observability/grafana/provisioning/datasources/datasources.yaml",
		"../../../deploy/observability/grafana/provisioning/dashboards/dashboards.yaml",
	} {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var parsed any
		if err := yaml.Unmarshal(content, &parsed); err != nil {
			t.Errorf("%s YAML 无效: %v", file, err)
		}
	}
	for _, file := range []string{
		"../../../deploy/observability/grafana/provisioning/dashboards/reagent-agent.json",
		"../../../deploy/observability/grafana/provisioning/dashboards/reagent-model.json",
		"../../../deploy/observability/grafana/provisioning/dashboards/reagent-tool.json",
	} {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var dashboard struct {
			Title  string `json:"title"`
			Panels []any  `json:"panels"`
		}
		if err := json.Unmarshal(content, &dashboard); err != nil {
			t.Errorf("%s JSON 无效: %v", file, err)
		}
		if dashboard.Title == "" || len(dashboard.Panels) == 0 {
			t.Errorf("%s 缺少标题或面板", file)
		}
	}
}

// TestTailCollectorKeepsAbnormalTraces 锁定 §13 Tail 策略的关键保留项。
func TestTailCollectorKeepsAbnormalTraces(t *testing.T) {
	content, err := os.ReadFile("../../../deploy/observability/otel-collector-tail.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"tail_sampling",
		"status_code: {status_codes: [ERROR]}",
		"values: [canceled, deadline_exceeded]",
		"values: [overflow]",
		"values: [contract_invalid]",
		"threshold_ms: 30000",
		"key: reagent.run.cost_usd",
		"sampling_percentage: 10",
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("otel-collector-tail.yaml 缺少 %q", want)
		}
	}
}
