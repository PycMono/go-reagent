package ai

// Protocol identifies the wire protocol used by a model platform.
type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
)

// PlatformConfig is one self-contained model platform profile.
type PlatformConfig struct {
	ID       string   `json:"id" yaml:"id" toml:"id"`
	Protocol Protocol `json:"protocol" yaml:"protocol" toml:"protocol"`
	BaseURL  string   `json:"baseURL" yaml:"baseURL" toml:"baseURL"`
	APIKey   string   `json:"apiKey" yaml:"apiKey" toml:"apiKey"`
	Model    string   `json:"model" yaml:"model" toml:"model"`
}
