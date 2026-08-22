package config

import (
	"strings"
	"testing"
)

// observabilityConfigDoc 构造一份满足其他必填项、仅 observability 不同的配置。
func observabilityConfigDoc(observability string) string {
	return `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"p","db":0,"pool_size":5},
		"observability":` + observability + `
	}`
}

func loadObservability(t *testing.T, observability string) (*Config, error) {
	t.Helper()
	return Load(writeConfig(t, observabilityConfigDoc(observability)))
}

func TestObservabilityDisabledByDefault(t *testing.T) {
	config, err := Load(writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"p","db":0,"pool_size":5}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Observability.Enabled {
		t.Fatal("observability.enabled 必须默认 false")
	}
}

func TestObservabilityDisabledIgnoresSubConfig(t *testing.T) {
	_, err := loadObservability(t, `{
		"enabled": false,
		"otlp": {"protocol": "carrier-pigeon"},
		"tracing": {"sampling_mode": "sideways", "sample_ratio": 0},
		"content": {"mode": "everything"}
	}`)
	if err != nil {
		t.Fatalf("disabled 时子配置应一律忽略: %v", err)
	}
}

func TestObservabilityEnabledAppliesDefaults(t *testing.T) {
	config, err := loadObservability(t, `{
		"enabled": true,
		"service_name": "go-reagent",
		"environment": "development",
		"otlp": {"endpoint": "http://127.0.0.1:4317", "insecure": true},
		"tracing": {"enabled": true},
		"metrics": {"enabled": true}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	observability := config.Observability
	switch {
	case observability.OTLP.Protocol != "grpc":
		t.Fatalf("protocol = %q", observability.OTLP.Protocol)
	case observability.OTLP.TimeoutSeconds != 5:
		t.Fatalf("timeout_seconds = %d", observability.OTLP.TimeoutSeconds)
	case observability.OTLP.MaxQueueSize != 2048:
		t.Fatalf("max_queue_size = %d", observability.OTLP.MaxQueueSize)
	case observability.OTLP.MaxExportBatchSize != 512:
		t.Fatalf("max_export_batch_size = %d", observability.OTLP.MaxExportBatchSize)
	case observability.Tracing.SamplingMode != "head":
		t.Fatalf("sampling_mode = %q", observability.Tracing.SamplingMode)
	case observability.Tracing.SampleRatio != 1.0:
		t.Fatalf("sample_ratio = %v", observability.Tracing.SampleRatio)
	case observability.Metrics.Host != "127.0.0.1":
		t.Fatalf("metrics.host = %q", observability.Metrics.Host)
	case observability.Metrics.Port != 9464:
		t.Fatalf("metrics.port = %d", observability.Metrics.Port)
	case observability.Metrics.Path != "/metrics":
		t.Fatalf("metrics.path = %q", observability.Metrics.Path)
	case observability.Metrics.DisableRuntimeMetrics:
		t.Fatal("runtime metrics 缺省必须启用")
	case observability.Content.Mode != "none":
		t.Fatalf("content.mode = %q", observability.Content.Mode)
	}
}

func TestObservabilityRejectsInvalidValues(t *testing.T) {
	base := func(extra string) string {
		return `{
			"enabled": true,
			"service_name": "go-reagent",
			"environment": "development",
			"otlp": {"endpoint": "http://127.0.0.1:4317", "insecure": true},
			"tracing": {"enabled": true` + extra + `}
		}`
	}
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{name: "missing service name", doc: `{"enabled":true,"tracing":{"enabled":false},"metrics":{"enabled":false}}`, want: "service_name"},
		{name: "sample ratio above one", doc: base(`,"sample_ratio":1.5`), want: "sample_ratio"},
		{name: "negative sample ratio", doc: base(`,"sample_ratio":-0.1`), want: "sample_ratio"},
		{name: "unknown sampling mode", doc: base(`,"sampling_mode":"sideways"`), want: "sampling_mode"},
		{name: "tail with ratio below one", doc: base(`,"sampling_mode":"tail","sample_ratio":0.5`), want: "tail"},
		{name: "non grpc protocol", doc: `{
			"enabled": true, "service_name": "go-reagent",
			"otlp": {"endpoint": "127.0.0.1:4317", "protocol": "http"},
			"tracing": {"enabled": false}
		}`, want: "protocol"},
		{name: "batch exceeds queue", doc: `{
			"enabled": true, "service_name": "go-reagent",
			"otlp": {"endpoint": "127.0.0.1:4317", "max_queue_size": 10, "max_export_batch_size": 20},
			"tracing": {"enabled": false}
		}`, want: "max_export_batch_size"},
		{name: "content mode not none", doc: `{
			"enabled": true, "service_name": "go-reagent",
			"tracing": {"enabled": false}, "content": {"mode": "redacted"}
		}`, want: "content.mode"},
		{name: "invalid metrics path", doc: `{
			"enabled": true, "service_name": "go-reagent",
			"tracing": {"enabled": false}, "metrics": {"enabled": true, "path": "metrics"}
		}`, want: "metrics.path"},
		{name: "invalid metrics port", doc: `{
			"enabled": true, "service_name": "go-reagent",
			"tracing": {"enabled": false}, "metrics": {"enabled": true, "port": 70000}
		}`, want: "metrics.port"},
		{name: "invalid endpoint", doc: `{
			"enabled": true, "service_name": "go-reagent",
			"otlp": {"endpoint": "not a target"}, "tracing": {"enabled": true}
		}`, want: "endpoint"},
		{name: "insecure public endpoint in production", doc: `{
			"enabled": true, "service_name": "go-reagent", "environment": "production",
			"otlp": {"endpoint": "collector.internal:4317", "insecure": true},
			"tracing": {"enabled": true}
		}`, want: "insecure"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadObservability(t, test.doc)
			if err == nil {
				t.Fatalf("期望包含 %q 的错误，实际成功", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误 %q 不包含 %q", err.Error(), test.want)
			}
		})
	}
}

// 显式 0 与未配置一样归一化为 1.0（与 SDK 归一化一致，§12）；需要 0%
// 采样时关闭 tracing.enabled。
func TestObservabilitySampleRatioZeroNormalizesToOne(t *testing.T) {
	config, err := loadObservability(t, `{
		"enabled": true, "service_name": "go-reagent", "environment": "development",
		"otlp": {"endpoint": "127.0.0.1:4317", "insecure": true},
		"tracing": {"enabled": true, "sample_ratio": 0}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if config.Observability.Tracing.SampleRatio != 1.0 {
		t.Fatalf("sample_ratio = %v, want 1.0", config.Observability.Tracing.SampleRatio)
	}
}

func TestObservabilityDisableRuntimeMetrics(t *testing.T) {
	config, err := loadObservability(t, `{
		"enabled": true, "service_name": "go-reagent",
		"tracing": {"enabled": false},
		"metrics": {"enabled": true, "disable_runtime_metrics": true}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Observability.Metrics.DisableRuntimeMetrics {
		t.Fatal("disable_runtime_metrics 必须可读")
	}
}

func TestObservabilityAcceptsTailWithFullRatio(t *testing.T) {
	_, err := loadObservability(t, `{
		"enabled": true, "service_name": "go-reagent", "environment": "staging",
		"otlp": {"endpoint": "collector.internal:4317", "insecure": true},
		"tracing": {"enabled": true, "sampling_mode": "tail", "sample_ratio": 1.0}
	}`)
	if err != nil {
		t.Fatalf("tail + ratio 1.0 + 非生产 insecure 应通过: %v", err)
	}
}

func TestObservabilityInsecureLoopbackAllowedInProduction(t *testing.T) {
	_, err := loadObservability(t, `{
		"enabled": true, "service_name": "go-reagent", "environment": "production",
		"otlp": {"endpoint": "127.0.0.1:4317", "insecure": true},
		"tracing": {"enabled": true}
	}`)
	if err != nil {
		t.Fatalf("loopback insecure 在生产应允许: %v", err)
	}
}

func TestObservabilityTracingDisabledSkipsEndpoint(t *testing.T) {
	_, err := loadObservability(t, `{
		"enabled": true, "service_name": "go-reagent",
		"tracing": {"enabled": false},
		"metrics": {"enabled": true}
	}`)
	if err != nil {
		t.Fatalf("tracing 关闭时不应要求 endpoint: %v", err)
	}
}
