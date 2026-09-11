package observability

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

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
