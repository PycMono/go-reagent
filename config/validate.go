package config

import (
	"errors"
	"net"
	"net/http"
	"net/textproto"
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
	return config.Redis.normalizeAndValidate()
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
		name := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(rawName))
		envName := strings.TrimSpace(rawEnv)
		if name == "" || envName == "" {
			return errors.New("mcp.servers.header_env 名称和值不能为空")
		}
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
