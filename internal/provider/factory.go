package provider

import (
	"errors"
	"fmt"
	"strings"
)

// Options contains the complete configuration for one Provider instance.
type Options struct {
	Name     string
	Protocol string
	BaseURL  string
	APIKey   string
	Model    string
}

// New creates an LLM Provider for an OpenAI- or Anthropic-compatible API.
func New(options Options) (LLMProvider, error) {
	options.Name = strings.TrimSpace(options.Name)
	options.Protocol = strings.ToLower(strings.TrimSpace(options.Protocol))
	options.BaseURL = strings.TrimSpace(options.BaseURL)
	options.APIKey = strings.TrimSpace(options.APIKey)
	options.Model = strings.TrimSpace(options.Model)

	if options.APIKey == "" {
		return nil, errors.New("apiKey 不能为空")
	}
	if options.Model == "" {
		return nil, errors.New("model 不能为空")
	}
	if options.BaseURL == "" {
		return nil, errors.New("baseURL 不能为空")
	}

	switch options.Protocol {
	case "openai":
		return newOpenAICompatibleProvider(
			options.APIKey,
			options.BaseURL,
			options.Model,
			options.Name,
		), nil
	case "anthropic":
		return newClaudeProvider(
			options.APIKey,
			options.BaseURL,
			options.Model,
			options.Name,
		), nil
	default:
		return nil, fmt.Errorf("不支持的 Provider protocol %q，可选值: openai, anthropic", options.Protocol)
	}
}
