// Package reagent exposes the synchronous go-reagent SDK.
package reagent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/jinzhu/configor"
)

const (
	DefaultHistoryMessageLimit = 100
)

type PlatformConfig = ai.PlatformConfig
type PricingConfig = ai.PricingConfig
type Protocol = ai.Protocol

const (
	ProtocolOpenAI    = ai.ProtocolOpenAI
	ProtocolAnthropic = ai.ProtocolAnthropic
)

// Config describes all configured model platforms and the active selection.
type Config struct {
	CurrentPlatform string             `json:"currentPlatform" yaml:"currentPlatform" toml:"currentPlatform"`
	Platforms       []PlatformConfig   `json:"platforms" yaml:"platforms" toml:"platforms"`
	Bot             BotConfig          `json:"bot" yaml:"bot" toml:"bot"`
	Conversation    ConversationConfig `json:"conversation" yaml:"conversation" toml:"conversation"`
	MySQL           MySQLConfig        `json:"mysql" yaml:"mysql" toml:"mysql"`
}

// ConversationConfig controls optional durable conversation history.
type ConversationConfig struct {
	Enabled             bool `json:"enabled" yaml:"enabled" toml:"enabled"`
	HistoryMessageLimit int  `json:"history_message_limit" yaml:"history_message_limit" toml:"history_message_limit"`
}

// MySQLConfig describes the conversation persistence connection pool.
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

// BotConfig describes optional external notification channels.
type BotConfig struct {
	WeCom WeComConfig `json:"wecom" yaml:"wecom" toml:"wecom"`
}

// WeComConfig configures outbound enterprise WeChat group notifications.
type WeComConfig struct {
	WebhookURL string `json:"webhookURL" yaml:"webhookURL" toml:"webhookURL"`
}

// LoadConfig decodes, normalizes, and validates configuration through Configor.
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if err := configor.Load(&cfg, path); err != nil {
		return nil, wrap(ErrorCodeConfigLoad, "LoadConfig", fmt.Errorf("加载配置 %s 失败: %w", path, err))
	}

	if err := cfg.normalizeAndValidate(); err != nil {
		return nil, wrap(ErrorCodeConfigInvalid, "LoadConfig", fmt.Errorf("加载配置 %s 失败: %w", path, err))
	}

	return &cfg, nil
}

// Current returns the selected platform profile.
func (c *Config) Current() (PlatformConfig, error) {
	if c == nil {
		return PlatformConfig{}, errors.New("配置不能为空")
	}

	for _, platform := range c.Platforms {
		if platform.ID != c.CurrentPlatform {
			continue
		}
		if platform.APIKey == "" {
			return PlatformConfig{}, fmt.Errorf("当前平台 %q 未配置 apiKey", platform.ID)
		}
		return platform, nil
	}

	return PlatformConfig{}, c.currentPlatformNotFoundError()
}

func (c *Config) currentPlatformNotFoundError() error {
	available := make([]string, 0, len(c.Platforms))
	for _, platform := range c.Platforms {
		available = append(available, platform.ID)
	}
	return fmt.Errorf(
		"当前平台 %q 不存在，可用平台: %s",
		c.CurrentPlatform,
		strings.Join(available, ", "),
	)
}
