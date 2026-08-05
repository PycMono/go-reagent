package pi

import (
	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

type Role = ai.Role
type Message = ai.Message
type ContentType = ai.ContentType
type ContentBlock = ai.ContentBlock
type ToolCall = ai.ToolCall
type RunRequest = agent.RunRequest
type RunResult = agent.RunResult
type ContextBlock = agent.ContextBlock

const (
	RoleSystem      = ai.RoleSystem
	RoleUser        = ai.RoleUser
	RoleAssistant   = ai.RoleAssistant
	RoleTool        = ai.RoleTool
	ContentTypeText = ai.ContentTypeText
)

func TextBlock(text string) ContentBlock { return ai.TextBlock(text) }

func UserMessage(text string) Message {
	return Message{Role: RoleUser, Content: []ContentBlock{TextBlock(text)}}
}

func SystemMessage(text string) Message {
	return Message{Role: RoleSystem, Content: []ContentBlock{TextBlock(text)}}
}
