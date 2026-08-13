// Package providers selects an official-SDK-backed AI provider by protocol.
package providers

import (
	"fmt"

	"github.com/PycMono/go-reagent/pi/ai"
)

// New validates opts and constructs its protocol-specific provider.
func New(opts Options) (ai.Provider, error) {
	if err := opts.NormalizeAndValidate(); err != nil {
		return nil, err
	}

	switch opts.Protocol {
	case ProtocolOpenAI:
		return NewOpenAi(opts), nil
	case ProtocolAnthropic:
		return NewAnthropic(opts), nil
	default:
		return nil, fmt.Errorf("不支持的 Provider protocol %q，可选值: openai, anthropic", opts.Protocol)
	}
}
