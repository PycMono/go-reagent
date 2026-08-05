package pi

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

// NormalizeAndValidate canonicalizes Config fields and validates all platforms.
func (c *Config) NormalizeAndValidate() error {
	if c == nil {
		return errors.New("配置不能为空")
	}
	c.CurrentPlatform = strings.TrimSpace(c.CurrentPlatform)
	if c.CurrentPlatform == "" {
		return errors.New("currentPlatform 不能为空")
	}
	if len(c.Platforms) == 0 {
		return errors.New("platforms 不能为空")
	}
	ids := make(map[string]struct{}, len(c.Platforms))
	for index := range c.Platforms {
		platform := &c.Platforms[index]
		normalizePlatform(platform)
		if err := validatePlatform(platform, index); err != nil {
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

func (c *Config) normalizeAndValidate() error { return c.NormalizeAndValidate() }

func normalizePlatform(platform *ai.PlatformConfig) {
	platform.ID = strings.TrimSpace(platform.ID)
	platform.Protocol = ai.Protocol(strings.ToLower(strings.TrimSpace(string(platform.Protocol))))
	platform.BaseURL = strings.TrimSpace(platform.BaseURL)
	platform.APIKey = strings.TrimSpace(platform.APIKey)
	platform.Model = strings.TrimSpace(platform.Model)
}

func validatePlatform(platform *ai.PlatformConfig, index int) error {
	prefix := fmt.Sprintf("platforms[%d]", index)
	if platform.ID == "" {
		return fmt.Errorf("%s.id 不能为空", prefix)
	}
	if platform.Protocol != ai.ProtocolOpenAI && platform.Protocol != ai.ProtocolAnthropic {
		return fmt.Errorf("%s.protocol %q 不受支持，可选值: %s, %s", prefix, platform.Protocol, ai.ProtocolOpenAI, ai.ProtocolAnthropic)
	}
	if err := normalizeBaseURL(platform); err != nil {
		return fmt.Errorf("%s.baseURL: %w", prefix, err)
	}
	if platform.Model == "" {
		return fmt.Errorf("%s.model 不能为空", prefix)
	}
	return validatePricing(platform.Pricing, prefix)
}

func validatePricing(pricing *ai.PricingConfig, prefix string) error {
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

func normalizeBaseURL(platform *ai.PlatformConfig) error {
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
