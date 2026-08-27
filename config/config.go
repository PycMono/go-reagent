package config

import (
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/loopdetect"
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
	Notice          NoticeConfig        `json:"notice" yaml:"notice" toml:"notice"`
	Permissions     PermissionsConfig   `json:"permissions" yaml:"permissions" toml:"permissions"`
	Tools           ToolsConfig         `json:"tools" yaml:"tools" toml:"tools"`
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
	WorkspaceDir string `json:"workspace_dir" yaml:"workspace_dir" toml:"workspace_dir"`
	// Limits 是运行预算；未配置（零值）的字段由 pi 层回填
	// governor.DefaultLimits（20 轮 / $1 / 2M tokens），config 不填默认。
	Limits governor.Limits `json:"limits" yaml:"limits" toml:"limits"`
	// LoopDetection 是工具循环护栏配置；整节可省略，零值即默认启用。
	LoopDetection loopdetect.Config `json:"loop_detection" yaml:"loop_detection" toml:"loop_detection"`
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

// NoticeConfig 是外部通知通道配置；通道 webhook 为空表示该通道关闭。
type NoticeConfig struct {
	WeCom NoticeWeComConfig `json:"wecom" yaml:"wecom" toml:"wecom"`
}

type NoticeWeComConfig struct {
	WebhookURL string `json:"webhook_url" yaml:"webhook_url" toml:"webhook_url"`
}

// PermissionsConfig 是 Tool 调用的权限策略。规则为空表示不启用权限拦截，
// ToolRuntime 保持默认中间件链。
type PermissionsConfig struct {
	Rules []PermissionRuleConfig `json:"rules" yaml:"rules" toml:"rules"`
}

// PermissionRuleConfig 是一条权限规则：工具名 + 一组参数正则（匹配
// tool_call 的原始参数 JSON），命中任一正则即按 Effect 处置。
type PermissionRuleConfig struct {
	Tool string `json:"tool" yaml:"tool" toml:"tool"`
	// Effect 本期仅接受 deny；allow/ask 预留给三态权限与人工审批，
	// 其他值在 Load 时启动失败（与 ObservabilityContentConfig.Mode 同惯例）。
	Effect string `json:"effect" yaml:"effect" toml:"effect"`
	// Patterns 至少一条，每条必须是合法正则（Load 时编译校验）。
	Patterns []string `json:"patterns" yaml:"patterns" toml:"patterns"`
	// Reason 是可选的拒绝原因，会随错误返回给模型阅读。
	Reason string `json:"reason" yaml:"reason" toml:"reason"`
}

// ToolsConfig 是 Tool 执行的可靠性策略。零值表示全部关闭：无超时兜底、
// 不重试，链保持纯默认。
type ToolsConfig struct {
	// TimeoutSeconds 是单次 Tool 执行的超时秒数（协作式取消），0 表示不启用。
	TimeoutSeconds int             `json:"timeout_seconds" yaml:"timeout_seconds" toml:"timeout_seconds"`
	Retry          ToolRetryConfig `json:"retry" yaml:"retry" toml:"retry"`
}

// ToolRetryConfig 是瞬态失败重试策略，只对白名单中的幂等工具生效。
type ToolRetryConfig struct {
	// Attempts 是总尝试次数（含首次），<=1 表示不重试；上限由 pi 层
	// middleware.MaxRetryAttempts 钳制。
	Attempts int `json:"attempts" yaml:"attempts" toml:"attempts"`
	// BackoffMs 是第 N 次重试前的等待毫秒基数（线性递增）；<=0 时由 pi 层
	// middleware.DefaultRetryBackoff 兜底。
	BackoffMs int `json:"backoff_ms" yaml:"backoff_ms" toml:"backoff_ms"`
	// Tools 是允许重试的工具白名单；Attempts>1 时应配置，否则重试不匹配任何工具。
	Tools []string `json:"tools" yaml:"tools" toml:"tools"`
}
