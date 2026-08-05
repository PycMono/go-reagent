package ai

// Protocol identifies the wire protocol used by a model platform.
type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
)

// PlatformConfig is one self-contained model platform profile.
type PlatformConfig struct {
	ID       string         `json:"id" yaml:"id" toml:"id"`
	Protocol Protocol       `json:"protocol" yaml:"protocol" toml:"protocol"`
	BaseURL  string         `json:"baseURL" yaml:"baseURL" toml:"baseURL"`
	APIKey   string         `json:"apiKey" yaml:"apiKey" toml:"apiKey"`
	Model    string         `json:"model" yaml:"model" toml:"model"`
	Pricing  *PricingConfig `json:"pricing" yaml:"pricing" toml:"pricing"`
}

// PricingConfig snapshots USD prices per one million tokens for a platform.
type PricingConfig struct {
	InputUSDPerMillionTokens  float64 `json:"input_usd_per_million_tokens" yaml:"input_usd_per_million_tokens" toml:"input_usd_per_million_tokens"`
	OutputUSDPerMillionTokens float64 `json:"output_usd_per_million_tokens" yaml:"output_usd_per_million_tokens" toml:"output_usd_per_million_tokens"`
}
