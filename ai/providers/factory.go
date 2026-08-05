// Package providers selects an official-SDK-backed AI client by protocol.
package providers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/ai/providers/anthropic"
	"github.com/PycMono/go-reagent/ai/providers/openai"
)

// New validates config and constructs its protocol-specific client.
func New(config ai.PlatformConfig) (ai.Client, error) {
	config.ID = strings.TrimSpace(config.ID)
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.Model = strings.TrimSpace(config.Model)

	if config.APIKey == "" {
		return nil, errors.New("apiKey 不能为空")
	}
	if config.Model == "" {
		return nil, errors.New("model 不能为空")
	}
	if config.BaseURL == "" {
		return nil, errors.New("baseURL 不能为空")
	}

	switch config.Protocol {
	case ai.ProtocolOpenAI:
		return openai.New(config), nil
	case ai.ProtocolAnthropic:
		return anthropic.New(config), nil
	default:
		return nil, fmt.Errorf("不支持的 Provider protocol %q，可选值: openai, anthropic", config.Protocol)
	}
}
