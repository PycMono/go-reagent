package chat

import (
	"context"

	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type runListener struct {
	runID  string
	events chan<- vo.RunEventVO
}

func newRunListener(runID string, events chan<- vo.RunEventVO) pi.EventListener {
	return &runListener{runID: runID, events: events}
}

func (listener *runListener) OnEvent(ctx context.Context, event pi.AgentEvent) {
	mapped, important, ok := mapRunEvent(listener.runID, event)
	if !ok {
		return
	}
	if !important {
		select {
		case listener.events <- mapped:
		default:
		}
		return
	}
	select {
	case listener.events <- mapped:
	case <-ctx.Done():
	}
}

func mapRunEvent(runID string, event pi.AgentEvent) (vo.RunEventVO, bool, bool) {
	result := vo.RunEventVO{RunID: runID}
	switch event.Type {
	case pi.AgentEventToolStart, pi.AgentEventToolUpdate, pi.AgentEventToolEnd:
		if event.Tool == nil {
			return vo.RunEventVO{}, false, false
		}
		if isSkillRead(event.Tool.Call.Name, event.Tool.Call.Arguments) {
			return vo.RunEventVO{}, false, false
		}
		result.Tool = &vo.ToolEventVO{
			ID: event.Tool.Call.ID, Name: event.Tool.Call.Name,
			Arguments: append([]byte(nil), event.Tool.Call.Arguments...),
		}
		switch event.Type {
		case pi.AgentEventToolStart:
			result.Type = vo.RunEventToolStarted
			return result, true, true
		case pi.AgentEventToolUpdate:
			result.Type = vo.RunEventToolUpdated
			if event.Tool.Update != nil && !isReadTool(event.Tool.Call.Name) {
				result.Tool.Content = mapAIContent(event.Tool.Update.Content)
				result.Tool.Details = event.Tool.Update.Details
			}
			return result, false, true
		case pi.AgentEventToolEnd:
			result.Type = vo.RunEventToolCompleted
			if !isReadTool(event.Tool.Call.Name) {
				result.Tool.Content = mapAIContent(event.Tool.Content)
				result.Tool.Details = event.Tool.Details
			}
			result.Tool.IsError = event.Tool.IsError
			result.Tool.ErrorCode = string(event.Tool.ErrorCode)
			return result, true, true
		}
	case pi.AgentEventMessageStart:
		result.Type = vo.RunEventMessageStarted
		return result, true, true
	case pi.AgentEventMessageUpdate:
		if event.Delta == nil {
			return vo.RunEventVO{}, false, false
		}
		result.Type = vo.RunEventMessageDelta
		delta := mapContentBlock(*event.Delta)
		result.Delta = &delta
		return result, true, true
	case pi.AgentEventMessageEnd:
		if event.Message == nil {
			return vo.RunEventVO{}, false, false
		}
		result.Type = vo.RunEventMessageCompleted
		result.Message = mapRunMessage(*event.Message)
		return result, true, true
	}
	return vo.RunEventVO{}, false, false
}

func mapAIContent(content []ai.ContentBlock) []vo.ContentBlockVO {
	result := make([]vo.ContentBlockVO, 0, len(content))
	for _, block := range content {
		result = append(result, mapContentBlock(block))
	}
	return result
}

// mapContentBlock 统一映射内容块；image 块只透传 URL。
func mapContentBlock(block ai.ContentBlock) vo.ContentBlockVO {
	result := vo.ContentBlockVO{Type: string(block.Type), Text: block.Text}
	if block.Image != nil {
		result.Image = &vo.ImageContentVO{URL: block.Image.URL}
	}
	return result
}

func mapRunMessage(message ai.Message) *vo.RunMessageVO {
	result := &vo.RunMessageVO{
		Role: string(message.Role), Content: mapAIContent(message.Content),
		ToolCallID: message.ToolCallID, ToolName: message.ToolName, IsError: message.IsError,
		ToolCalls: make([]vo.ToolCallVO, 0, len(message.ToolCalls)),
	}
	onlySkillReads := len(message.ToolCalls) > 0
	for _, call := range message.ToolCalls {
		if isSkillRead(call.Name, call.Arguments) {
			continue
		}
		onlySkillReads = false
		result.ToolCalls = append(result.ToolCalls, vo.ToolCallVO{
			ID: call.ID, Name: call.Name, Arguments: append([]byte(nil), call.Arguments...),
		})
	}
	if onlySkillReads {
		return nil
	}
	return result
}
