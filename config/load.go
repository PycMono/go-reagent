package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/jinzhu/configor"
)

type fileConfig struct {
	CurrentPlatform string              `json:"currentPlatform" yaml:"currentPlatform" toml:"currentPlatform"`
	Platforms       []ai.PlatformConfig `json:"platforms" yaml:"platforms" toml:"platforms"`
	Bot             BotConfig           `json:"bot" yaml:"bot" toml:"bot"`
	Conversation    ConversationConfig  `json:"conversation" yaml:"conversation" toml:"conversation"`
	MySQL           MySQLConfig         `json:"mysql" yaml:"mysql" toml:"mysql"`
}

// Load decodes the existing flattened service configuration.
func Load(path string) (*Config, error) {
	var input fileConfig
	if err := configor.Load(&input, path); err != nil {
		return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
	}
	config := &Config{
		Pi: pi.Config{
			CurrentPlatform: input.CurrentPlatform,
			Platforms:       input.Platforms,
		},
		Bot:          input.Bot,
		Conversation: input.Conversation,
		MySQL:        input.MySQL,
	}
	if err := config.Pi.NormalizeAndValidate(); err != nil {
		return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
	}
	if err := config.normalizeAndValidate(); err != nil {
		return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
	}
	return config, nil
}

// NewFromEnvironment loads CONFIG_PATH, defaulting to config.json.
func NewFromEnvironment() (*Config, error) {
	path := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	if path == "" {
		path = "config.json"
	}
	return Load(path)
}

// NewPlatform returns the selected model platform for Pi's Fx graph.
func NewPlatform(config *Config) (ai.PlatformConfig, error) {
	if config == nil {
		return ai.PlatformConfig{}, fmt.Errorf("配置不能为空")
	}
	return config.Pi.Current()
}

// NewPiConfig returns a defensive Pi configuration copy.
func NewPiConfig(config *Config) *pi.Config {
	if config == nil {
		return nil
	}
	cloned := config.Pi
	cloned.Platforms = append([]ai.PlatformConfig(nil), config.Pi.Platforms...)
	for index := range cloned.Platforms {
		if config.Pi.Platforms[index].Pricing != nil {
			pricing := *config.Pi.Platforms[index].Pricing
			cloned.Platforms[index].Pricing = &pricing
		}
	}
	return &cloned
}
