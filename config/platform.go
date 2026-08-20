package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
)

// CurrentPlatformOptions returns the selected model platform.
func (config *Config) CurrentPlatformOptions() (providers.Options, error) {
	for _, platform := range config.Platforms {
		if platform.ID != config.CurrentPlatform {
			continue
		}
		if platform.APIKey == "" {
			return providers.Options{}, fmt.Errorf("当前平台 %q 未配置 apiKey", platform.ID)
		}
		return platform, nil
	}

	available := make([]string, 0, len(config.Platforms))
	for _, platform := range config.Platforms {
		available = append(available, platform.ID)
	}
	return providers.Options{}, fmt.Errorf("当前平台 %q 不存在，可用平台: %s", config.CurrentPlatform, strings.Join(available, ", "))
}

func (config *Config) normalizeAndValidatePlatforms() error {
	config.CurrentPlatform = strings.TrimSpace(config.CurrentPlatform)
	if config.CurrentPlatform == "" {
		return errors.New("currentPlatform 不能为空")
	}
	if len(config.Platforms) == 0 {
		return errors.New("platforms 不能为空")
	}

	ids := make(map[string]struct{}, len(config.Platforms))
	for index := range config.Platforms {
		platform := &config.Platforms[index]
		normalizePlatform(platform)
		if err := validatePlatform(platform, index); err != nil {
			return err
		}
		if _, exists := ids[platform.ID]; exists {
			return fmt.Errorf("platforms[%d].id %q 重复", index, platform.ID)
		}
		ids[platform.ID] = struct{}{}
	}

	_, err := config.CurrentPlatformOptions()
	return err
}

func normalizePlatform(platform *providers.Options) {
	platform.ID = strings.TrimSpace(platform.ID)
	platform.Protocol = providers.Protocol(strings.ToLower(strings.TrimSpace(string(platform.Protocol))))
	platform.BaseURL = strings.TrimSpace(platform.BaseURL)
	platform.APIKey = strings.TrimSpace(platform.APIKey)
	platform.Model = strings.TrimSpace(platform.Model)
}

func validatePlatform(platform *providers.Options, index int) error {
	prefix := fmt.Sprintf("platforms[%d]", index)
	if platform.ID == "" {
		return fmt.Errorf("%s.id 不能为空", prefix)
	}
	if platform.Protocol != providers.ProtocolOpenAI && platform.Protocol != providers.ProtocolAnthropic {
		return fmt.Errorf("%s.protocol %q 不受支持，可选值: %s, %s", prefix, platform.Protocol, providers.ProtocolOpenAI, providers.ProtocolAnthropic)
	}
	if err := normalizeBaseURL(platform); err != nil {
		return fmt.Errorf("%s.baseURL: %w", prefix, err)
	}
	if platform.Model == "" {
		return fmt.Errorf("%s.model 不能为空", prefix)
	}
	if platform.ContextWindowTokens < 0 {
		return fmt.Errorf("%s.contextWindowTokens 不能为负数", prefix)
	}
	return validatePricing(platform.Pricing, prefix)
}

func validatePricing(pricing *providers.Pricing, prefix string) error {
	if pricing == nil {
		return fmt.Errorf("%s.pricing 不能为空", prefix)
	}
	if math.IsNaN(pricing.InputUSDPerMillionTokens) || math.IsInf(pricing.InputUSDPerMillionTokens, 0) ||
		pricing.InputUSDPerMillionTokens < 0 || pricing.InputUSDPerMillionTokens >= ai.MaxUsageDecimalExclusive {
		return fmt.Errorf("%s.pricing.input_usd_per_million_tokens 必须是小于 %.0f 的有限非负数", prefix, float64(ai.MaxUsageDecimalExclusive))
	}
	if math.IsNaN(pricing.OutputUSDPerMillionTokens) || math.IsInf(pricing.OutputUSDPerMillionTokens, 0) ||
		pricing.OutputUSDPerMillionTokens < 0 || pricing.OutputUSDPerMillionTokens >= ai.MaxUsageDecimalExclusive {
		return fmt.Errorf("%s.pricing.output_usd_per_million_tokens 必须是小于 %.0f 的有限非负数", prefix, float64(ai.MaxUsageDecimalExclusive))
	}
	return nil
}

func normalizeBaseURL(platform *providers.Options) error {
	parsed, err := url.Parse(platform.BaseURL)
	if err != nil {
		return fmt.Errorf("不是合法 URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("必须是带 Host 的 HTTP/HTTPS 地址")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("不能包含用户信息、查询参数或片段")
	}
	platform.BaseURL = strings.TrimRight(platform.BaseURL, "/") + "/"
	return nil
}
