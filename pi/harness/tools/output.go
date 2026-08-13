package tools

import (
	"github.com/PycMono/go-reagent/pi/ai"
)

func textToolOutput(text string) ai.ToolOutput {
	if text == "" {
		return ai.ToolOutput{}
	}
	return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(text)}}
}
