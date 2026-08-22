package providers

import (
	"errors"
	"strings"
)

// Protocol identifies the wire protocol used by a model platform.
type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
)

// Options is one self-contained model platform profile.
type Options struct {
	ID       string   `json:"id" yaml:"id" toml:"id"`
	Protocol Protocol `json:"protocol" yaml:"protocol" toml:"protocol"`
	BaseURL  string   `json:"baseURL" yaml:"baseURL" toml:"baseURL"`
	APIKey   string   `json:"apiKey" yaml:"apiKey" toml:"apiKey"`
	Model    string   `json:"model" yaml:"model" toml:"model"`
	Pricing  *Pricing `json:"pricing" yaml:"pricing" toml:"pricing"`
	// ContextWindowTokens 是模型的上下文窗口容量；0 表示未声明，主动压缩保持关闭。
	ContextWindowTokens int64 `json:"contextWindowTokens,omitempty" yaml:"contextWindowTokens" toml:"contextWindowTokens"`
}

// Pricing snapshots USD prices per one million tokens for a platform.
//
// 缓存价格（阶段 4）用指针区分“未配置”与显式 0：Provider 上报缓存 Token
// 而未配置对应价格时，该次调用成本只能标记为 estimated（§9.1）。
type Pricing struct {
	InputUSDPerMillionTokens      float64  `json:"input_usd_per_million_tokens" yaml:"input_usd_per_million_tokens" toml:"input_usd_per_million_tokens"`
	OutputUSDPerMillionTokens     float64  `json:"output_usd_per_million_tokens" yaml:"output_usd_per_million_tokens" toml:"output_usd_per_million_tokens"`
	CacheReadUSDPerMillionTokens  *float64 `json:"cache_read_usd_per_million_tokens,omitempty" yaml:"cache_read_usd_per_million_tokens" toml:"cache_read_usd_per_million_tokens"`
	CacheWriteUSDPerMillionTokens *float64 `json:"cache_write_usd_per_million_tokens,omitempty" yaml:"cache_write_usd_per_million_tokens" toml:"cache_write_usd_per_million_tokens"`
}

// NormalizeAndValidate canonicalizes and validates the fields required to create a provider.
func (opts *Options) NormalizeAndValidate() error {
	opts.ID = strings.TrimSpace(opts.ID)
	opts.BaseURL = strings.TrimSpace(opts.BaseURL)
	opts.APIKey = strings.TrimSpace(opts.APIKey)
	opts.Model = strings.TrimSpace(opts.Model)

	if opts.APIKey == "" {
		return errors.New("apiKey 不能为空")
	}
	if opts.Model == "" {
		return errors.New("model 不能为空")
	}
	if opts.BaseURL == "" {
		return errors.New("baseURL 不能为空")
	}
	if opts.ContextWindowTokens < 0 {
		return errors.New("contextWindowTokens 不能为负数")
	}
	return nil
}
