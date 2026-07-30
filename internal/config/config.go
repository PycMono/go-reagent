// Package config loads and validates the go-reagent process configuration.
package config

import (
	"errors"
	"fmt"
	"net/url"
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

func (c *Config) normalizeAndValidate() error {
	c.CurrentPlatform = strings.TrimSpace(c.CurrentPlatform)
	if c.CurrentPlatform == "" {
		return errors.New("currentPlatform 不能为空")
	}
	if len(c.Platforms) == 0 {
		return errors.New("platforms 不能为空")
	}
	if err := c.Bot.normalizeAndValidate(); err != nil {
		return err
	}

	ids := make(map[string]struct{}, len(c.Platforms))
	for index := range c.Platforms {
		platform := &c.Platforms[index]
		platform.normalize()
		if err := platform.validate(index); err != nil {
			return err
		}
		if _, exists := ids[platform.ID]; exists {
			return fmt.Errorf("platforms[%d].id %q 重复", index, platform.ID)
		}
		ids[platform.ID] = struct{}{}
	}

	current, err := c.Current()
	if err != nil {
		return err
	}
	if current.APIKey == "" {
		return fmt.Errorf("当前平台 %q 未配置 apiKey", current.ID)
	}
	return nil
}

func (c *BotConfig) normalizeAndValidate() error {
	c.WeCom.WebhookURL = strings.TrimSpace(c.WeCom.WebhookURL)
	if c.WeCom.WebhookURL == "" {
		return nil
	}
	parsed, err := url.Parse(c.WeCom.WebhookURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("bot.wecom.webhookURL 必须是带 Host 的 HTTPS URL")
	}
	return nil
}

func (p *PlatformConfig) normalize() {
	p.ID = strings.TrimSpace(p.ID)
	p.Protocol = strings.ToLower(strings.TrimSpace(p.Protocol))
	p.BaseURL = strings.TrimSpace(p.BaseURL)
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.Model = strings.TrimSpace(p.Model)
}

func (p *PlatformConfig) validate(index int) error {
	prefix := fmt.Sprintf("platforms[%d]", index)
	if p.ID == "" {
		return fmt.Errorf("%s.id 不能为空", prefix)
	}
	if p.Protocol != ProtocolOpenAI && p.Protocol != ProtocolAnthropic {
		return fmt.Errorf("%s.protocol %q 不受支持，可选值: %s, %s", prefix, p.Protocol, ProtocolOpenAI, ProtocolAnthropic)
	}
	if err := p.normalizeBaseURL(); err != nil {
		return fmt.Errorf("%s.baseURL: %w", prefix, err)
	}
	if p.Model == "" {
		return fmt.Errorf("%s.model 不能为空", prefix)
	}
	return nil
}

func (p *PlatformConfig) normalizeBaseURL() error {
	parsed, err := url.Parse(p.BaseURL)
	if err != nil {
		return fmt.Errorf("不是合法 URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("必须是带 Host 的 HTTP/HTTPS 地址")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("不能包含用户信息、查询参数或片段")
	}
	p.BaseURL = strings.TrimRight(p.BaseURL, "/") + "/"
	return nil
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
