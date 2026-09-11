package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PycMono/go-reagent/pi"
)

func (config *Config) normalizeAndValidate(options loadOptions) error {
	if err := config.normalizeAgentPlatform(); err != nil {
		return err
	}
	if err := config.normalizeAndValidatePlatforms(); err != nil {
		return err
	}
	if err := config.Agent.normalizeAndValidate(options); err != nil {
		return err
	}
	if err := config.Identity.normalizeAndValidate(); err != nil {
		return err
	}
	if err := config.MCP.normalizeAndValidate(); err != nil {
		return err
	}
	config.HTTP.normalize()
	if config.SnowflakeNodeID < 0 || config.SnowflakeNodeID > 1023 {
		return errors.New("snowflake_node_id 必须在 0 到 1023 之间")
	}
	if err := config.Notice.normalizeAndValidate(); err != nil {
		return err
	}
	if err := config.Permissions.normalizeAndValidate(); err != nil {
		return err
	}
	// Tools 的数值边界（重试上限、退避默认值）由 pi 层 middleware 兜底，
	// config 不做第二套校验。
	if err := config.Conversation.normalizeAndValidate(&config.MySQL); err != nil {
		return err
	}
	if err := config.Observability.normalizeAndValidate(); err != nil {
		return err
	}
	return config.Redis.normalizeAndValidate()
}

// envNamePattern 是 MCP stdio 子进程环境变量名的合法形式。
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envReferencePattern 匹配完整 ${NAME} 形式的环境引用；部分插值
// （如 prefix-${NAME}）不匹配，按字面量处理。
var envReferencePattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

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
	ratio := config.SampleRatio
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
		return errors.New("observability.tracing.sample_ratio 必须在 (0,1] 区间内")
	}
	// 0 或未配置归一化为 1.0，与 go-observability-sdk 的归一化行为一致；
	// 需要 0% 采样时应关闭 tracing.enabled，不得依赖 0（§12）。
	if ratio == 0 {
		config.SampleRatio = 1.0
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
	config.Path = strings.TrimSpace(config.Path)
	if config.Path == "" {
		config.Path = defaultObservabilityMetricsPath
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
	for index := range config.Servers {
		server := &config.Servers[index]
		if !server.Enabled {
			continue
		}
		server.Name = strings.TrimSpace(server.Name)
		if server.Name == "" {
			return errors.New("mcp.servers.name 不能为空")
		}
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
	server.Transport = strings.TrimSpace(strings.ToLower(server.Transport))
	switch server.Transport {
	case "http":
		return server.normalizeHTTPTransport()
	case "stdio":
		return server.normalizeStdioTransport()
	case "":
		return errors.New("mcp.servers.transport 必填，只能是 http 或 stdio")
	default:
		return errors.New("mcp.servers.transport 只能是 http 或 stdio")
	}
}

// normalizeHTTPTransport 校验 http 分支：url 必填，stdio 专属字段必须为空。
func (server *MCPServerConfig) normalizeHTTPTransport() error {
	if server.Command != "" || len(server.Args) > 0 || len(server.Env) > 0 || strings.TrimSpace(server.CWD) != "" {
		return errors.New("mcp.servers transport=http 时不能配置 command/args/env/cwd")
	}
	server.URL = strings.TrimSpace(server.URL)
	parsed, err := url.Parse(server.URL)
	if err != nil || parsed.Host == "" {
		return errors.New("mcp.servers.url 必须是绝对 URL")
	}
	// SSRF 边界：非 loopback 地址必须使用 https。
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
	// Timeout 不填默认值：<=0 时由 pi 层（mcp.DefaultTimeout）兜底。
	if err := server.normalizeHeaders(); err != nil {
		return err
	}
	return server.normalizeCommon()
}

// normalizeStdioTransport 校验 stdio 分支：command 必填，http 专属字段必须为空。
func (server *MCPServerConfig) normalizeStdioTransport() error {
	if strings.TrimSpace(server.URL) != "" || len(server.HeaderEnv) > 0 {
		return errors.New("mcp.servers stdio transport 不能配置 url/header_env")
	}
	server.Command = strings.TrimSpace(server.Command)
	if server.Command == "" {
		return errors.New("mcp.servers.command 不能为空")
	}
	if err := rejectNUL("mcp.servers.command", server.Command); err != nil {
		return err
	}
	for index, arg := range server.Args {
		if err := rejectNUL(fmt.Sprintf("mcp.servers.args[%d]", index), arg); err != nil {
			return err
		}
	}
	// cwd 为空时继承进程工作目录；相对路径相对进程工作目录解析，
	// 校验通过后写回绝对路径，下游装配层不再解析。
	server.CWD = strings.TrimSpace(server.CWD)
	if err := rejectNUL("mcp.servers.cwd", server.CWD); err != nil {
		return err
	}
	if server.CWD != "" {
		resolved, err := resolveDirectory(server.CWD)
		if err != nil {
			return fmt.Errorf("mcp.servers.cwd %q 无效: %w", server.CWD, err)
		}
		server.CWD = resolved
	}
	if err := server.normalizeEnv(); err != nil {
		return err
	}
	return server.normalizeCommon()
}

func (server *MCPServerConfig) normalizeCommon() error {
	if err := server.normalizeAllowedTools(); err != nil {
		return err
	}
	server.ToolPrefix = strings.TrimSpace(server.ToolPrefix)
	return nil
}

// normalizeEnv 校验子进程环境覆盖：名称必须合法；值为字面量或完整 ${NAME}
// 引用。引用沿用 header_env 的“存在且非空”规则，错误只包含变量名。
func (server *MCPServerConfig) normalizeEnv() error {
	for name, value := range server.Env {
		if !envNamePattern.MatchString(name) {
			return fmt.Errorf("mcp.servers.env 环境变量名 %q 非法", name)
		}
		if match := envReferencePattern.FindStringSubmatch(value); match != nil {
			if resolved, exists := os.LookupEnv(match[1]); !exists || strings.TrimSpace(resolved) == "" {
				return fmt.Errorf("mcp.servers.env 引用的环境变量 %q 未设置或为空", match[1])
			}
		}
	}
	return nil
}

func rejectNUL(field string, value string) error {
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s 不能包含 NUL 字节", field)
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
		if trimmedName == "" || envName == "" {
			return errors.New("mcp.servers.header_env 名称和值不能为空")
		}
		name := http.CanonicalHeaderKey(trimmedName)
		if _, denied := blocked[http.CanonicalHeaderKey(name)]; denied {
			return errors.New("mcp.servers.header_env 不能覆盖协议控制 Header")
		}
		normalized[name] = envName
	}
	// 结构校验全部通过后，再检查引用的环境变量存在且非空（fail-fast：
	// 启动期暴露部署遗漏，而不是首个 MCP 请求才失败）。
	for _, envName := range normalized {
		if value, exists := os.LookupEnv(envName); !exists || strings.TrimSpace(value) == "" {
			return fmt.Errorf("mcp.servers.header_env 引用的环境变量 %q 未设置或为空", envName)
		}
	}
	server.HeaderEnv = normalized
	return nil
}

func (server *MCPServerConfig) normalizeAllowedTools() error {
	if len(server.AllowTools) == 0 {
		return errors.New("mcp.servers.allow_tools 不能为空")
	}
	for index := range server.AllowTools {
		server.AllowTools[index] = strings.TrimSpace(server.AllowTools[index])
	}
	return nil
}

func (config *AgentConfig) normalizeAndValidate(options loadOptions) error {
	config.WorkspaceDir = strings.TrimSpace(config.WorkspaceDir)
	if config.WorkspaceDir == "" {
		config.WorkspaceDir = DefaultAgentWorkspaceDir
	}
	resolved, err := resolveAgentWorkspaceDir(config.WorkspaceDir, options.allowProcessCWD)
	if err != nil {
		return err
	}
	// 把解析后的绝对路径写回配置，下游装配层不再做任何解析与校验。
	config.WorkspaceDir = resolved
	if config.WorkspacePolicy != nil {
		if err := pi.ValidateWorkspacePolicy(resolved, config.WorkspacePolicy.PI()); err != nil {
			return fmt.Errorf("agent.workspace_policy: %w", err)
		}
	}
	// Limits 的默认值回填在 pi 层（governor.New）：未配置字段使用
	// governor.DefaultLimits。这里只做 Load 期 fail-fast 的固有校验；
	// loop_detection 的防御性归一化也在 pi 层（loopdetect.normalize）。
	return config.Limits.Validate()
}

// resolveAgentWorkspaceDir 校验 Workspace 目录必须存在、是目录；默认还拒绝
// 等于进程当前目录（防止 Agent 工具直接读写服务自身工作目录），
// allowProcessCWD 为 true 时跳过该拒绝（仅 CLI 类入口使用，见
// WithAllowProcessCWD）。返回解析后的绝对路径。
func resolveAgentWorkspaceDir(path string, allowProcessCWD bool) (string, error) {
	resolved, err := resolveDirectory(path)
	if err != nil {
		return "", fmt.Errorf("agent.workspace_dir %q 无效: %w", path, err)
	}
	if allowProcessCWD {
		return resolved, nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("agent.workspace_dir: 获取进程当前目录失败: %w", err)
	}
	resolvedWorkingDir, err := resolveDirectory(workingDir)
	if err != nil {
		return "", fmt.Errorf("agent.workspace_dir: 解析进程当前目录失败: %w", err)
	}
	if resolved == resolvedWorkingDir {
		return "", fmt.Errorf("agent.workspace_dir %q 不能使用进程当前目录", path)
	}
	return resolved, nil
}

func resolveDirectory(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析绝对路径: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("解析真实路径: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("必须是目录")
	}
	return filepath.Clean(resolved), nil
}

// normalize 只填默认值；端口是否可监听由启动期 listen 报错，config 不预判。
func (config *HTTPConfig) normalize() {
	config.Host = strings.TrimSpace(config.Host)
	if config.Host == "" {
		config.Host = "127.0.0.1"
	}
	config.Port = strings.TrimSpace(config.Port)
	if config.Port == "" {
		config.Port = "8080"
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30
	}
}

func (config *ConversationConfig) normalizeAndValidate(mysql *MySQLConfig) error {
	if config.HistoryMessageLimit == 0 {
		config.HistoryMessageLimit = DefaultHistoryMessageLimit
	}
	if !config.Enabled {
		return nil
	}
	return mysql.normalizeAndValidate()
}

// normalizeAndValidate 只保证地址可用；db、pool_size 等数值边界交给
// go-redis 的默认值与连接期报错。
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
	return nil
}

// normalizeAndValidate 只校验连接必填项；连接池、日志等数值边界交给
// 驱动/gorm 的默认值与运行期报错。
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
	default:
		return nil
	}
}

func (config *NoticeConfig) normalizeAndValidate() error {
	config.WeCom.WebhookURL = strings.TrimSpace(config.WeCom.WebhookURL)
	if config.WeCom.WebhookURL == "" {
		return nil
	}
	parsed, err := url.Parse(config.WeCom.WebhookURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("notice.wecom.webhook_url 必须是带 Host 的 HTTPS URL")
	}
	return nil
}

// PermissionEffectDeny 是本期唯一支持的权限处置；allow/ask 预留。
const PermissionEffectDeny = "deny"

func (config *PermissionsConfig) normalizeAndValidate() error {
	for index := range config.Rules {
		rule := &config.Rules[index]
		rule.Tool = strings.TrimSpace(rule.Tool)
		rule.Effect = strings.TrimSpace(rule.Effect)
		rule.Reason = strings.TrimSpace(rule.Reason)
		if rule.Tool == "" {
			return fmt.Errorf("permissions.rules[%d].tool 不能为空", index)
		}
		if rule.Effect != PermissionEffectDeny {
			return fmt.Errorf("permissions.rules[%d].effect 本期仅接受 %q", index, PermissionEffectDeny)
		}
		if len(rule.Patterns) == 0 {
			return fmt.Errorf("permissions.rules[%d].patterns 至少配置一条", index)
		}
		for _, pattern := range rule.Patterns {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("permissions.rules[%d] 正则 %q 编译失败: %w", index, pattern, err)
			}
		}
	}
	return nil
}
