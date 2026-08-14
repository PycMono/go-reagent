package chat

import (
	"context"

	"github.com/PycMono/go-reagent/common/vo"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type runReporter struct {
	runID  string
	events chan<- vo.RunEventVO
}

func newRunReporter(runID string, events chan<- vo.RunEventVO) pi.Reporter {
	return &runReporter{runID: runID, events: events}
}

func (reporter *runReporter) Report(ctx context.Context, event pi.AgentEvent) {
	mapped, important, ok := mapRunEvent(reporter.runID, event)
	if !ok {
		return
	}
	if !important {
		select {
		case reporter.events <- mapped:
		default:
		}
		return
	}
	select {
	case reporter.events <- mapped:
	case <-ctx.Done():
	}
}

func mapRunEvent(runID string, event pi.AgentEvent) (vo.RunEventVO, bool, bool) {
	result := vo.RunEventVO{RunID: runID}
	switch event.Type {
	case pi.AgentEventThinking:
		result.Type = vo.RunEventAgentThinking
		return result, false, true
	case pi.AgentEventToolStart, pi.AgentEventToolUpdate, pi.AgentEventToolEnd:
		if event.Tool == nil {
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
			if event.Tool.Update != nil {
				result.Tool.Content = mapAIContent(event.Tool.Update.Content)
				result.Tool.Details = event.Tool.Update.Details
			}
			return result, false, true
		case pi.AgentEventToolEnd:
			result.Type = vo.RunEventToolCompleted
			if event.Tool.Result != nil {
				result.Tool.ID = event.Tool.Result.ToolCallID
				result.Tool.Name = event.Tool.Result.ToolName
				result.Tool.Content = mapAIContent(event.Tool.Result.Content)
				result.Tool.Details = event.Tool.Result.Details
				result.Tool.IsError = event.Tool.Result.IsError
				result.Tool.ErrorCode = string(event.Tool.Result.ErrorCode)
			}
			return result, true, true
		}
	case pi.AgentEventMessage:
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
		result = append(result, vo.ContentBlockVO{Type: string(block.Type), Text: block.Text})
	}
	return result
}

func mapRunMessage(message ai.Message) *vo.RunMessageVO {
	result := &vo.RunMessageVO{
		Role: string(message.Role), Content: mapAIContent(message.Content),
		ToolCallID: message.ToolCallID, ToolName: message.ToolName, IsError: message.IsError,
		ToolCalls: make([]vo.ToolCallVO, 0, len(message.ToolCalls)),
	}
	for _, call := range message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, vo.ToolCallVO{
			ID: call.ID, Name: call.Name, Arguments: append([]byte(nil), call.Arguments...),
		})
	}
	return result
}
