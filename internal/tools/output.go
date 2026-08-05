package tools

import (
	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
)

func textToolOutput(text string) agent.ToolOutput {
	if text == "" {
		return agent.ToolOutput{}
	}
	return agent.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(text)}}
}
