package config

import "github.com/PycMono/go-reagent/pi/ai/providers"

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
}

type AgentConfig struct {
	WorkspaceDir string `json:"workspace_dir" yaml:"workspace_dir" toml:"workspace_dir"`
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
