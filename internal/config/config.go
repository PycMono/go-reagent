// Package config loads and validates the go-reagent process configuration.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jinzhu/configor"
)

const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
)

// Config describes all configured model platforms and the active selection.
type Config struct {
	CurrentPlatform string           `json:"currentPlatform" yaml:"currentPlatform" toml:"currentPlatform"`
	Platforms       []PlatformConfig `json:"platforms" yaml:"platforms" toml:"platforms"`
	Bot             BotConfig        `json:"bot" yaml:"bot" toml:"bot"`
}

// BotConfig describes optional external notification channels.
type BotConfig struct {
	WeCom WeComConfig `json:"wecom" yaml:"wecom" toml:"wecom"`
}

// WeComConfig configures outbound enterprise WeChat group notifications.
type WeComConfig struct {
	WebhookURL string `json:"webhookURL" yaml:"webhookURL" toml:"webhookURL"`
}

// PlatformConfig is one self-contained model platform profile.
type PlatformConfig struct {
	ID       string `json:"id" yaml:"id" toml:"id"`
	Protocol string `json:"protocol" yaml:"protocol" toml:"protocol"`
	BaseURL  string `json:"baseURL" yaml:"baseURL" toml:"baseURL"`
	APIKey   string `json:"apiKey" yaml:"apiKey" toml:"apiKey"`
	Model    string `json:"model" yaml:"model" toml:"model"`
}

// Load decodes, normalizes, and validates configuration through Configor.
func Load(path string) (*Config, error) {
	var cfg Config
	if err := configor.Load(&cfg, path); err != nil {
		return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
	}

	if err := cfg.normalizeAndValidate(); err != nil {
		return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
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
