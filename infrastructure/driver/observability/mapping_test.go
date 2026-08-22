package observability

import (
	"context"
	"testing"
	"time"

	sdkobservability "github.com/PycMono/go-observability-sdk"
	sdkmetrics "github.com/PycMono/go-observability-sdk/metrics"
	"github.com/PycMono/go-reagent/config"
	piobservability "github.com/PycMono/go-reagent/pi/harness/observability"
)

func mappedConfig(t *testing.T, mutate func(*config.ObservabilityConfig)) config.ObservabilityConfig {
	t.Helper()
	cfg := config.ObservabilityConfig{
		Enabled:     true,
		ServiceName: "go-reagent",
		Environment: "development",
		OTLP: config.ObservabilityOTLPConfig{
			Endpoint:           "127.0.0.1:4317",
			Protocol:           "grpc",
			Insecure:           true,
			TimeoutSeconds:     7,
			MaxQueueSize:       100,
			MaxExportBatchSize: 50,
		},
		Tracing: config.ObservabilityTracingConfig{
			Enabled:      true,
			SamplingMode: "head",
			SampleRatio:  0.5,
		},
		Metrics: config.ObservabilityMetricsConfig{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    9464,
			Path:    "/metrics",
		},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return cfg
}

func TestToObservabilityConfigMapsAllFields(t *testing.T) {
	mapped := ToObservabilityConfig(mappedConfig(t, nil), "1.2.3")
	switch {
	case !mapped.Enabled || !mapped.Tracing.Enabled || !mapped.Metrics.Enabled:
		t.Fatalf("Enabled 映射错误: %#v", mapped)
	case mapped.ServiceName != "go-reagent" || mapped.Version != "1.2.3" || mapped.Environment != "development":
		t.Fatalf("Resource 字段映射错误: %#v", mapped)
	case mapped.Tracing.Endpoint != "127.0.0.1:4317" || !mapped.Tracing.Insecure:
		t.Fatalf("Tracing 端点映射错误: %#v", mapped.Tracing)
	case mapped.Tracing.SampleRatio != 0.5:
		t.Fatalf("SampleRatio = %v", mapped.Tracing.SampleRatio)
	case mapped.Tracing.Timeout != 7*time.Second:
		t.Fatalf("Timeout = %v", mapped.Tracing.Timeout)
	case mapped.Tracing.MaxQueueSize != 100 || mapped.Tracing.MaxExportBatchSize != 50:
		t.Fatalf("Queue/Batch 映射错误: %#v", mapped.Tracing)
	case mapped.Metrics.Port != "9464" || mapped.Metrics.Host != "127.0.0.1" || mapped.Metrics.Path != "/metrics":
		t.Fatalf("Metrics 映射错误: %#v", mapped.Metrics)
	case !mapped.Metrics.RuntimeMetrics:
		t.Fatal("RuntimeMetrics 缺省必须映射为 true")
	}
}

func TestToObservabilityConfigDisabledPassthrough(t *testing.T) {
	mapped := ToObservabilityConfig(config.ObservabilityConfig{Enabled: false}, "")
	if mapped.Enabled || mapped.Tracing.Enabled || mapped.Metrics.Enabled {
		t.Fatalf("disabled 配置不应开启子系统: %#v", mapped)
	}
}

// TestDisabledRuntimeIsFullyNoop 验证阶段 0 验收：关闭时无网络、无 Metrics
// Listener、无兜底 ID，SDK 返回功能完整的 Noop Runtime。
func TestDisabledRuntimeIsFullyNoop(t *testing.T) {
	runtime, err := sdkobservability.New(context.Background(), ToObservabilityConfig(config.ObservabilityConfig{Enabled: false}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.InstallGlobal(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDomainDefinitionsPassSDKValidation(t *testing.T) {
	definitions := piobservability.DomainMetricDefinitions()
	if len(definitions) == 0 {
		t.Fatal("领域指标定义不能为空")
	}
	if _, err := sdkmetrics.ValidateDefinitions(piobservability.ForbiddenLabelKeys, definitions); err != nil {
		t.Fatalf("领域 Definition 必须通过 SDK 校验（含基数红线）: %v", err)
	}
}
