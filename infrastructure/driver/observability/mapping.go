// Package observability 把 go-reagent 的项目配置映射为
// go-observability-sdk 的 Runtime 配置，注册领域 Metric Definition 与
// Label 基数红线，并在阶段 1 接入 Fx 生命周期。
//
// 本包不重复创建 TracerProvider、MeterProvider、Exporter、Resource、
// W3C Propagator 或 Metrics Listener——这些全部由 go-observability-sdk
package observability

import (
	"strconv"
	"time"

	sdkobservability "github.com/PycMono/go-observability-sdk"
	"github.com/PycMono/go-reagent/config"
)

// ToObservabilityConfig 把已通过校验的项目配置映射为 go-observability-sdk Config。
// 配置非法时 config.Load 已在 Fx 启动前失败，本函数不再重复校验。
// version 写入 Resource 的 service.version。
func ToObservabilityConfig(cfg config.ObservabilityConfig, version string) sdkobservability.Config {
	mapped := sdkobservability.Config{
		Enabled:     cfg.Enabled,
		ServiceName: cfg.ServiceName,
		Version:     version,
		Environment: cfg.Environment,
	}
	if !cfg.Enabled {
		return mapped
	}

	mapped.Tracing = sdkobservability.TracingConfig{
		Enabled:            cfg.Tracing.Enabled,
		Endpoint:           cfg.OTLP.Endpoint,
		Insecure:           cfg.OTLP.Insecure,
		SampleRatio:        cfg.Tracing.SampleRatio,
		Timeout:            time.Duration(cfg.OTLP.TimeoutSeconds) * time.Second,
		MaxQueueSize:       cfg.OTLP.MaxQueueSize,
		MaxExportBatchSize: cfg.OTLP.MaxExportBatchSize,
	}
	mapped.Metrics = sdkobservability.MetricsConfig{
		Enabled:        cfg.Metrics.Enabled,
		Host:           cfg.Metrics.Host,
		Port:           strconv.Itoa(cfg.Metrics.Port),
		Path:           cfg.Metrics.Path,
		RuntimeMetrics: !cfg.Metrics.DisableRuntimeMetrics,
	}
	return mapped
}
