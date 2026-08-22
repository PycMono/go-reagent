package config

import (
	"errors"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const defaultMCPTimeoutSeconds = 60

var mcpNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (config *Config) normalizeAndValidate() error {
	if err := config.normalizeAndValidatePlatforms(); err != nil {
		return err
	}
	config.Agent.normalize()
	if err := config.Agent.validate(); err != nil {
		return err
	}
	if err := config.MCP.normalizeAndValidate(); err != nil {
		return err
	}
	if err := config.HTTP.normalizeAndValidate(); err != nil {
		return err
	}
	if config.SnowflakeNodeID < 0 || config.SnowflakeNodeID > 1023 {
		return errors.New("snowflake_node_id 必须在 0 到 1023 之间")
	}
	if err := config.Bot.normalizeAndValidate(); err != nil {
		return err
	}
	if err := config.Conversation.normalizeAndValidate(&config.MySQL); err != nil {
		return err
	}
	if err := config.Observability.normalizeAndValidate(); err != nil {
		return err
	}
	return config.Redis.normalizeAndValidate()
}

// observability 默认值与 go-observability-sdk v1.0.1 保持一致（§12）。
const (
	defaultObservabilityTimeoutSeconds     = 5
	defaultObservabilityMaxQueueSize       = 2048
	defaultObservabilityMaxExportBatchSize = 512
	defaultObservabilityMetricsHost        = "127.0.0.1"
	defaultObservabilityMetricsPort        = 9464
	defaultObservabilityMetricsPath        = "/metrics"
)

// observabilityNonProductionEnvironments 是允许 Insecure OTLP Endpoint 的
// 非生产环境白名单（与 go-observability-sdk 一致）。
var observabilityNonProductionEnvironments = map[string]struct{}{
	"local": {}, "dev": {}, "development": {}, "test": {}, "testing": {}, "staging": {},
}

func (config *ObservabilityConfig) normalizeAndValidate() error {
	if !config.Enabled {
		return nil
	}
	config.ServiceName = strings.TrimSpace(config.ServiceName)
	if config.ServiceName == "" {
		return errors.New("observability.service_name 不能为空")
	}
	config.Environment = strings.TrimSpace(config.Environment)
	if err := config.OTLP.normalizeAndValidate(); err != nil {
		return err
	}
	if err := config.Tracing.normalizeAndValidate(); err != nil {
		return err
	}
	if err := config.Metrics.normalizeAndValidate(); err != nil {
		return err
	}
	if err := config.Content.normalizeAndValidate(); err != nil {
		return err
	}
	// 启用 Tracing 时 Endpoint 必须合法；Insecure 只允许 Loopback 或明确的
	// 非生产环境（§12）。
	if config.Tracing.Enabled {
		if !validObservabilityOTLPTarget(config.OTLP.Endpoint) {
			return errors.New("observability.otlp.endpoint 必须是合法的 OTLP/gRPC 目标")
		}
		if config.OTLP.Insecure && !isObservabilityLoopback(config.OTLP.Endpoint) && !isObservabilityNonProduction(config.Environment) {
			return errors.New("observability.otlp.insecure 只允许 loopback 或非生产环境")
		}
	}
	return nil
}

func (config *ObservabilityOTLPConfig) normalizeAndValidate() error {
	config.Endpoint = strings.TrimSpace(config.Endpoint)
	// OTLP gRPC Exporter 只接受 host:port 目标；剥掉用户配置里可能携带的
	// http(s):// scheme，避免 "too many colons in address" 导出失败。
	config.Endpoint = strings.TrimPrefix(config.Endpoint, "http://")
	config.Endpoint = strings.TrimPrefix(config.Endpoint, "https://")
	config.Protocol = strings.TrimSpace(config.Protocol)
	if config.Protocol == "" {
		config.Protocol = "grpc"
	}
	if config.Protocol != "grpc" {
		return errors.New("observability.otlp.protocol 本期只能是 grpc")
	}
	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = defaultObservabilityTimeoutSeconds
	}
	if config.MaxQueueSize == 0 {
		config.MaxQueueSize = defaultObservabilityMaxQueueSize
	}
	if config.MaxExportBatchSize == 0 {
		config.MaxExportBatchSize = defaultObservabilityMaxExportBatchSize
	}
	if config.TimeoutSeconds < 0 || config.MaxQueueSize < 0 || config.MaxExportBatchSize < 0 {
		return errors.New("observability.otlp 的 timeout_seconds、max_queue_size、max_export_batch_size 必须为正数")
	}
	if config.MaxExportBatchSize > config.MaxQueueSize {
		return errors.New("observability.otlp.max_export_batch_size 不能大于 max_queue_size")
	}
	return nil
}

func (config *ObservabilityTracingConfig) normalizeAndValidate() error {
	config.SamplingMode = strings.TrimSpace(config.SamplingMode)
	if config.SamplingMode == "" {
		config.SamplingMode = "head"
	}
	if config.SamplingMode != "head" && config.SamplingMode != "tail" {
		return errors.New("observability.tracing.sampling_mode 只能是 head 或 tail")
	}
	if config.SampleRatio == nil {
		ratio := 1.0
		config.SampleRatio = &ratio
	} else {
		ratio := *config.SampleRatio
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
			return errors.New("observability.tracing.sample_ratio 必须在 (0,1] 区间内")
		}
		// go-observability-sdk v1.0.1 会把 0 归一化为 1.0；需要 0% 采样时应关闭
		// Tracing，不得用 0 表示（§12）。
		if ratio == 0 {
			return errors.New("observability.tracing.sample_ratio 不允许显式 0：需要 0% 采样请关闭 tracing")
		}
		if config.SamplingMode == "tail" && ratio != 1.0 {
			return errors.New("observability.tracing.sampling_mode=tail 时 sample_ratio 必须为 1.0")
		}
	}
	for index, upstream := range config.TrustedUpstreams {
		config.TrustedUpstreams[index] = strings.TrimSpace(upstream)
		if err := parseTrustedUpstream(config.TrustedUpstreams[index]); err != nil {
			return errors.New("observability.tracing.trusted_upstreams 必须是合法的 IP 或 CIDR")
		}
	}
	return nil
}

// parseTrustedUpstream 接受单个 IP 或 CIDR。
func parseTrustedUpstream(value string) error {
	if value == "" {
		return errors.New("empty upstream")
	}
	if strings.Contains(value, "/") {
		_, _, err := net.ParseCIDR(value)
		return err
	}
	if ip := net.ParseIP(value); ip == nil {
		return errors.New("invalid IP")
	}
	return nil
}

func (config *ObservabilityMetricsConfig) normalizeAndValidate() error {
	config.Host = strings.TrimSpace(config.Host)
	if config.Host == "" {
		config.Host = defaultObservabilityMetricsHost
	}
	if config.Port == 0 {
		config.Port = defaultObservabilityMetricsPort
	}
	if config.Port < 1 || config.Port > 65535 {
		return errors.New("observability.metrics.port 必须在 1 到 65535 之间")
	}
	config.Path = strings.TrimSpace(config.Path)
	if config.Path == "" {
		config.Path = defaultObservabilityMetricsPath
	}
	if !strings.HasPrefix(config.Path, "/") || strings.Contains(config.Path, "//") ||
		strings.ContainsAny(config.Path, "?#*") {
		return errors.New("observability.metrics.path 必须以 / 开头且不能包含 query、fragment、通配符或重复斜杠")
	}
	if config.RuntimeMetrics == nil {
		enabled := true
		config.RuntimeMetrics = &enabled
	}
	return nil
}

func (config *ObservabilityContentConfig) normalizeAndValidate() error {
	config.Mode = strings.TrimSpace(config.Mode)
	if config.Mode == "" {
		config.Mode = "none"
	}
	if config.Mode != "none" {
		return errors.New("observability.content.mode 本期只能是 none")
	}
	return nil
}

// validObservabilityOTLPTarget 校验 OTLP/gRPC Target：host[:port] 或带
// http(s)/dns scheme 的形式，与 go-observability-sdk 的判定保持一致。
func validObservabilityOTLPTarget(endpoint string) bool {
	if host, _, err := net.SplitHostPort(endpoint); err == nil {
		return host != ""
	}
	trimmed := strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
	trimmed = strings.TrimPrefix(trimmed, "dns:///")
	if trimmed == "" || strings.ContainsAny(trimmed, " \t/") {
		return false
	}
	return true
}

func isObservabilityLoopback(endpoint string) bool {
	host := endpoint
	if h, _, err := net.SplitHostPort(endpoint); err == nil {
		host = h
	} else {
		host = strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")
		host = strings.TrimPrefix(host, "dns:///")
		if index := strings.Index(host, "/"); index >= 0 {
			host = host[:index]
		}
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isObservabilityNonProduction(environment string) bool {
	_, ok := observabilityNonProductionEnvironments[strings.ToLower(strings.TrimSpace(environment))]
	return ok
}

func (config *MCPConfig) normalizeAndValidate() error {
	seenNames := make(map[string]struct{})
	for index := range config.Servers {
		server := &config.Servers[index]
		if !server.Enabled {
			continue
		}
		server.Name = strings.TrimSpace(server.Name)
		if server.Name == "" || !mcpNamePattern.MatchString(server.Name) {
			return errors.New("mcp.servers.name 必须是有效名称")
		}
		if _, exists := seenNames[server.Name]; exists {
			return errors.New("mcp.servers.name 不能重复")
		}
		seenNames[server.Name] = struct{}{}
		if !server.Required {
			return errors.New("mcp.servers.required 一期必须为 true")
		}
		if err := server.normalizeAndValidate(); err != nil {
			return err
		}
	}
	return nil
}

func (server *MCPServerConfig) normalizeAndValidate() error {
	server.URL = strings.TrimSpace(server.URL)
	parsed, err := url.Parse(server.URL)
	if err != nil || parsed.Host == "" {
		return errors.New("mcp.servers.url 必须是绝对 URL")
	}
	if parsed.User != nil {
		return errors.New("mcp.servers.url 不能包含 userinfo")
	}
	if parsed.RawQuery != "" {
		return errors.New("mcp.servers.url 不能包含 query")
	}
	if parsed.Fragment != "" {
		return errors.New("mcp.servers.url 不能包含 fragment")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return errors.New("mcp.servers.url 非 loopback 地址必须使用 https")
		}
	default:
		return errors.New("mcp.servers.url 必须使用 http 或 https")
	}
	if server.Timeout < 0 {
		return errors.New("mcp.servers.timeout 不能小于 0")
	}
	if server.Timeout == 0 {
		server.Timeout = defaultMCPTimeoutSeconds
	}
	if err := server.normalizeHeaders(); err != nil {
		return err
	}
	if err := server.normalizeAllowedTools(); err != nil {
		return err
	}
	server.ToolPrefix = strings.TrimSpace(server.ToolPrefix)
	if server.ToolPrefix != "" && !mcpNamePattern.MatchString(server.ToolPrefix) {
		return errors.New("mcp.servers.tool_prefix 必须是有效工具名前缀")
	}
	return nil
}

func (server *MCPServerConfig) normalizeHeaders() error {
	blocked := map[string]struct{}{
		"Host": {}, "Content-Length": {}, "Mcp-Session-Id": {}, "Content-Type": {}, "Accept": {},
	}
	normalized := make(map[string]string, len(server.HeaderEnv))
	for rawName, rawEnv := range server.HeaderEnv {
		trimmedName := strings.TrimSpace(rawName)
		envName := strings.TrimSpace(rawEnv)
		if !validMCPHeaderName(trimmedName) || envName == "" {
			return errors.New("mcp.servers.header_env 名称和值不能为空")
		}
		name := http.CanonicalHeaderKey(trimmedName)
		if _, denied := blocked[http.CanonicalHeaderKey(name)]; denied {
			return errors.New("mcp.servers.header_env 不能覆盖协议控制 Header")
		}
		if _, exists := normalized[name]; exists {
			return errors.New("mcp.servers.header_env 不能包含重复 Header")
		}
		normalized[name] = envName
	}
	server.HeaderEnv = normalized
	return nil
}

func validMCPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character))) {
			return false
		}
	}
	return true
}

func (server *MCPServerConfig) normalizeAllowedTools() error {
	if len(server.AllowTools) == 0 {
		return errors.New("mcp.servers.allow_tools 不能为空")
	}
	seen := make(map[string]struct{}, len(server.AllowTools))
	normalized := make([]string, 0, len(server.AllowTools))
	for _, rawName := range server.AllowTools {
		name := strings.TrimSpace(rawName)
		if name == "" || !mcpNamePattern.MatchString(name) {
			return errors.New("mcp.servers.allow_tools 包含无效工具名")
		}
		if _, exists := seen[name]; exists {
			return errors.New("mcp.servers.allow_tools 不能重复")
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	server.AllowTools = normalized
	return nil
}

func (config *AgentConfig) normalize() {
	config.WorkspaceDir = strings.TrimSpace(config.WorkspaceDir)
	if config.WorkspaceDir == "" {
		config.WorkspaceDir = DefaultAgentWorkspaceDir
	}
}

// validate 拒绝非法额度和全零安全策略。bundled service 不允许裸奔；
// SDK 调用方自行决定 Limits，config 只约束本服务。
func (config *AgentConfig) validate() error {
	limits := config.Limits
	switch {
	case limits.MaxTurns < 0:
		return errors.New("agent.limits.max_turns 不能小于 0")
	case limits.MaxTotalTokens < 0:
		return errors.New("agent.limits.max_total_tokens 不能小于 0")
	case limits.MaxCostUSD < 0 || math.IsNaN(limits.MaxCostUSD) || math.IsInf(limits.MaxCostUSD, 0):
		return errors.New("agent.limits.max_cost_usd 必须是有限非负数")
	case limits.MaxTurns == 0 && limits.MaxCostUSD == 0 && limits.MaxTotalTokens == 0:
		return errors.New("agent.limits 不允许全零：必须配置非零运行预算")
	}
	return nil
}

func (config *HTTPConfig) normalizeAndValidate() error {
	config.Host = strings.TrimSpace(config.Host)
	if config.Host == "" {
		config.Host = "127.0.0.1"
	}
	config.Port = strings.TrimSpace(config.Port)
	if config.Port == "" {
		config.Port = "8080"
	}
	if _, err := net.LookupPort("tcp", config.Port); err != nil {
		return errors.New("http.port 必须是有效端口")
	}
	if config.ReadTimeout < 0 {
		return errors.New("http.read_timeout 不能小于 0")
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30
	}
	if config.WriteTimeout < 0 {
		return errors.New("http.write_timeout 不能小于 0")
	}
	return nil
}

func (config *ConversationConfig) normalizeAndValidate(mysql *MySQLConfig) error {
	if config.HistoryMessageLimit == 0 {
		config.HistoryMessageLimit = DefaultHistoryMessageLimit
	}
	if config.HistoryMessageLimit < 1 {
		return errors.New("conversation.history_message_limit 必须大于 0")
	}
	if !config.Enabled {
		return nil
	}
	return mysql.normalizeAndValidate()
}

func (config *RedisConfig) normalizeAndValidate() error {
	if len(config.Addr) == 0 {
		return errors.New("redis.addr 不能为空")
	}
	for index := range config.Addr {
		config.Addr[index] = strings.TrimSpace(config.Addr[index])
		if config.Addr[index] == "" {
			return errors.New("redis.addr 不能包含空地址")
		}
	}
	if config.DB < 0 {
		return errors.New("redis.db 不能小于 0")
	}
	if config.PoolSize < 1 {
		return errors.New("redis.pool_size 必须大于 0")
	}
	return nil
}

func (config *MySQLConfig) normalizeAndValidate() error {
	config.Host = strings.TrimSpace(config.Host)
	config.Database = strings.TrimSpace(config.Database)
	config.User = strings.TrimSpace(config.User)
	switch {
	case config.Host == "":
		return errors.New("mysql.host 不能为空")
	case config.Port < 1 || config.Port > 65535:
		return errors.New("mysql.port 必须在 1 到 65535 之间")
	case config.Database == "":
		return errors.New("mysql.database 不能为空")
	case config.User == "":
		return errors.New("mysql.user 不能为空")
	case config.Password == "":
		return errors.New("mysql.password 不能为空")
	case config.MaxOpen < 1:
		return errors.New("mysql.max_open 必须大于 0")
	case config.MaxIdle < 0 || config.MaxIdle > config.MaxOpen:
		return errors.New("mysql.max_idle 必须在 0 到 mysql.max_open 之间")
	case config.ConnLifetime < 1:
		return errors.New("mysql.conn_lifetime 必须大于 0")
	case config.ConnTimeout < 1:
		return errors.New("mysql.conn_timeout 必须大于 0")
	case config.LogLevel < 1 || config.LogLevel > 4:
		return errors.New("mysql.log_level 必须在 1 到 4 之间")
	case config.SlowThreshold < 0:
		return errors.New("mysql.slow_threshold 不能小于 0")
	default:
		return nil
	}
}

func (config *BotConfig) normalizeAndValidate() error {
	config.WeCom.WebhookURL = strings.TrimSpace(config.WeCom.WebhookURL)
	if config.WeCom.WebhookURL == "" {
		return nil
	}
	parsed, err := url.Parse(config.WeCom.WebhookURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("bot.wecom.webhookURL 必须是带 Host 的 HTTPS URL")
	}
	return nil
}
