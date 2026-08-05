package pi

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

type PlatformConfig = ai.PlatformConfig
type PricingConfig = ai.PricingConfig
type Protocol = ai.Protocol

const (
	ProtocolOpenAI    = ai.ProtocolOpenAI
	ProtocolAnthropic = ai.ProtocolAnthropic
)

// Config describes the model platforms available to one Pi Agent.
type Config struct {
	CurrentPlatform string           `json:"currentPlatform" yaml:"currentPlatform" toml:"currentPlatform"`
	Platforms       []PlatformConfig `json:"platforms" yaml:"platforms" toml:"platforms"`
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
	return fmt.Errorf("当前平台 %q 不存在，可用平台: %s", c.CurrentPlatform, strings.Join(available, ", "))
}
