package config

import "github.com/PycMono/go-reagent/pi/ai/providers"

const DefaultHistoryMessageLimit = 100

// Config is the go-reagent business-service configuration.
type Config struct {
	CurrentPlatform string              `json:"currentPlatform" yaml:"currentPlatform" toml:"currentPlatform"`
	Platforms       []providers.Options `json:"platforms" yaml:"platforms" toml:"platforms"`
	Bot             BotConfig           `json:"bot" yaml:"bot" toml:"bot"`
	Conversation    ConversationConfig  `json:"conversation" yaml:"conversation" toml:"conversation"`
	MySQL           MySQLConfig         `json:"mysql" yaml:"mysql" toml:"mysql"`
	SnowflakeNodeID int                 `json:"snowflake_node_id" yaml:"snowflake_node_id" toml:"snowflake_node_id"`
}

type ConversationConfig struct {
	Enabled             bool `json:"enabled" yaml:"enabled" toml:"enabled"`
	HistoryMessageLimit int  `json:"history_message_limit" yaml:"history_message_limit" toml:"history_message_limit"`
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
