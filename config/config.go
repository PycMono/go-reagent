package config

import (
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai/providers"
)

const (
	DefaultHistoryMessageLimit = 100
	DefaultAgentWorkspaceDir   = "./workspaces/chat"
)

// Config is the go-reagent business-service configuration.
type Config struct {
	CurrentPlatform string              `json:"currentPlatform" yaml:"currentPlatform" toml:"currentPlatform"`
	Platforms       []providers.Options `json:"platforms" yaml:"platforms" toml:"platforms"`
	HTTP            HTTPConfig          `json:"http" yaml:"http" toml:"http"`
	Agent           AgentConfig         `json:"agent" yaml:"agent" toml:"agent"`
	MCP             MCPConfig           `json:"mcp" yaml:"mcp" toml:"mcp"`
	Bot             BotConfig           `json:"bot" yaml:"bot" toml:"bot"`
	Conversation    ConversationConfig  `json:"conversation" yaml:"conversation" toml:"conversation"`
	Redis           RedisConfig         `json:"redis" yaml:"redis" toml:"redis"`
	MySQL           MySQLConfig         `json:"mysql" yaml:"mysql" toml:"mysql"`
	SnowflakeNodeID int                 `json:"snowflake_node_id" yaml:"snowflake_node_id" toml:"snowflake_node_id"`
	Observability   ObservabilityConfig `json:"observability" yaml:"observability" toml:"observability"`
}

// ObservabilityConfig 是可观测性配置（设计 §12）。Enabled 默认 false，
// 为 false 时子配置一律忽略且不校验。
type ObservabilityConfig struct {
	Enabled     bool                       `json:"enabled" yaml:"enabled" toml:"enabled"`
	ServiceName string                     `json:"service_name" yaml:"service_name" toml:"service_name"`
	Environment string                     `json:"environment" yaml:"environment" toml:"environment"`
	OTLP        ObservabilityOTLPConfig    `json:"otlp" yaml:"otlp" toml:"otlp"`
	Tracing     ObservabilityTracingConfig `json:"tracing" yaml:"tracing" toml:"tracing"`
	Metrics     ObservabilityMetricsConfig `json:"metrics" yaml:"metrics" toml:"metrics"`
	Content     ObservabilityContentConfig `json:"content" yaml:"content" toml:"content"`
}

type ObservabilityOTLPConfig struct {
	Endpoint           string `json:"endpoint" yaml:"endpoint" toml:"endpoint"`
	Protocol           string `json:"protocol" yaml:"protocol" toml:"protocol"`
	Insecure           bool   `json:"insecure" yaml:"insecure" toml:"insecure"`
	TimeoutSeconds     int    `json:"timeout_seconds" yaml:"timeout_seconds" toml:"timeout_seconds"`
	MaxQueueSize       int    `json:"max_queue_size" yaml:"max_queue_size" toml:"max_queue_size"`
	MaxExportBatchSize int    `json:"max_export_batch_size" yaml:"max_export_batch_size" toml:"max_export_batch_size"`
}

type ObservabilityTracingConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled" toml:"enabled"`
	// SamplingMode 只接受 head 或 tail（§13）。
	SamplingMode string `json:"sampling_mode" yaml:"sampling_mode" toml:"sampling_mode"`
	// SampleRatio 为 0 或未配置时归一化为 1.0（与 go-observability-sdk
	// 一致）；需要 0% 采样请关闭 tracing.enabled。合法区间 (0,1]。
	SampleRatio float64 `json:"sample_ratio" yaml:"sample_ratio" toml:"sample_ratio"`
	// TrustedUpstreams 是可信上游的 IP 或 CIDR 列表（§7）：仅这些来源的请求
	// 可以保留 Remote Parent（traceparent/tracestate），其他公网请求先剥离
	// Trace Context 再创建内部 root Span。缺省为空，即不信任任何上游。
	TrustedUpstreams []string `json:"trusted_upstreams" yaml:"trusted_upstreams" toml:"trusted_upstreams"`
}

type ObservabilityMetricsConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled" toml:"enabled"`
	Host    string `json:"host" yaml:"host" toml:"host"`
	Port    int    `json:"port" yaml:"port" toml:"port"`
	Path    string `json:"path" yaml:"path" toml:"path"`
	// DisableRuntimeMetrics 显式关闭 Go Runtime Metrics；零值 false 即默认
	// 启用（§12 本项目默认 true）。用反向字段而非指针区分“未配置”与显式关闭。
	DisableRuntimeMetrics bool `json:"disable_runtime_metrics" yaml:"disable_runtime_metrics" toml:"disable_runtime_metrics"`
}

type ObservabilityContentConfig struct {
	// Mode 本期仅接受 none（§11）；其他值启动失败。
	Mode string `json:"mode" yaml:"mode" toml:"mode"`
}

type AgentConfig struct {
	WorkspaceDir string       `json:"workspace_dir" yaml:"workspace_dir" toml:"workspace_dir"`
	Limits       pi.RunLimits `json:"limits" yaml:"limits" toml:"limits"`
	// EnableContextPrune 显式启用主动上下文压缩的 L1 只读工具结果裁剪。
	EnableContextPrune bool `json:"enable_context_prune" yaml:"enable_context_prune" toml:"enable_context_prune"`
}

type MCPConfig struct {
	Servers []MCPServerConfig `json:"servers" yaml:"servers" toml:"servers"`
}

type MCPServerConfig struct {
	Name       string            `json:"name" yaml:"name" toml:"name"`
	Enabled    bool              `json:"enabled" yaml:"enabled" toml:"enabled"`
	Required   bool              `json:"required" yaml:"required" toml:"required"`
	URL        string            `json:"url" yaml:"url" toml:"url"`
	Timeout    int               `json:"timeout" yaml:"timeout" toml:"timeout"`
	HeaderEnv  map[string]string `json:"header_env" yaml:"header_env" toml:"header_env"`
	AllowTools []string          `json:"allow_tools" yaml:"allow_tools" toml:"allow_tools"`
	ToolPrefix string            `json:"tool_prefix" yaml:"tool_prefix" toml:"tool_prefix"`
}

type HTTPConfig struct {
	Host          string `json:"host" yaml:"host" toml:"host"`
	Port          string `json:"port" yaml:"port" toml:"port"`
	ReadTimeout   int    `json:"read_timeout" yaml:"read_timeout" toml:"read_timeout"`
	WriteTimeout  int    `json:"write_timeout" yaml:"write_timeout" toml:"write_timeout"`
	SecureCookies bool   `json:"secure_cookies" yaml:"secure_cookies" toml:"secure_cookies"`
}

type ConversationConfig struct {
	Enabled             bool `json:"enabled" yaml:"enabled" toml:"enabled"`
	HistoryMessageLimit int  `json:"history_message_limit" yaml:"history_message_limit" toml:"history_message_limit"`
}

type RedisConfig struct {
	Addr     []string `json:"addr" yaml:"addr" toml:"addr"`
	Password string   `json:"password" yaml:"password" toml:"password"`
	DB       int      `json:"db" yaml:"db" toml:"db"`
	PoolSize int      `json:"pool_size" yaml:"pool_size" toml:"pool_size"`
}

type MySQLConfig struct {
	Host          string `json:"host" yaml:"host" toml:"host"`
	Port          int    `json:"port" yaml:"port" toml:"port"`
	Database      string `json:"database" yaml:"database" toml:"database"`
	User          string `json:"user" yaml:"user" toml:"user"`
	Password      string `json:"password" yaml:"password" toml:"password"`
	MaxOpen       int    `json:"max_open" yaml:"max_open" toml:"max_open"`
	MaxIdle       int    `json:"max_idle" yaml:"max_idle" toml:"max_idle"`
	ConnLifetime  int    `json:"conn_lifetime" yaml:"conn_lifetime" toml:"conn_lifetime"`
	ConnTimeout   int    `json:"conn_timeout" yaml:"conn_timeout" toml:"conn_timeout"`
	LogLevel      int    `json:"log_level" yaml:"log_level" toml:"log_level"`
	SlowThreshold int    `json:"slow_threshold" yaml:"slow_threshold" toml:"slow_threshold"`
}

type BotConfig struct {
	WeCom WeComConfig `json:"wecom" yaml:"wecom" toml:"wecom"`
}

type WeComConfig struct {
	WebhookURL string `json:"webhookURL" yaml:"webhookURL" toml:"webhookURL"`
}
