package conversation

import (
	"fmt"
	"strconv"
	"strings"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

func messagesToDomain(messages []ai.Message, runID string) []*conversationentity.Message {
	if messages == nil {
		return nil
	}
	converted := make([]*conversationentity.Message, len(messages))
	for index := range messages {
		message := messages[index]
		converted[index] = &conversationentity.Message{
			RunID: runID,
			Role:  conversationentity.Role(message.Role),
			Payload: conversationentity.MessagePayload{
				Content:    make([]conversationentity.ContentBlock, len(message.Content)),
				ToolCalls:  make([]conversationentity.ToolCall, len(message.ToolCalls)),
				ToolCallID: message.ToolCallID,
				ToolName:   message.ToolName,
				IsError:    message.IsError,
			},
		}
		if message.Content == nil {
			converted[index].Payload.Content = nil
		}
		for contentIndex := range message.Content {
			converted[index].Payload.Content[contentIndex] = conversationentity.ContentBlock{
				Type: conversationentity.ContentType(message.Content[contentIndex].Type),
				Text: message.Content[contentIndex].Text,
			}
		}
		if message.ToolCalls == nil {
			converted[index].Payload.ToolCalls = nil
		}
		for callIndex := range message.ToolCalls {
			converted[index].Payload.ToolCalls[callIndex] = conversationentity.ToolCall{
				ID:        message.ToolCalls[callIndex].ID,
				Name:      message.ToolCalls[callIndex].Name,
				Arguments: append([]byte(nil), message.ToolCalls[callIndex].Arguments...),
			}
		}
	}
	return converted
}

func messagesToHistory(messages []*conversationentity.Message) ([]pi.Message, error) {
	if messages == nil {
		return nil, nil
	}
	converted := make([]pi.Message, 0, len(messages))
	for index, message := range messages {
		if message == nil {
			continue
		}

		var senderType string
		switch message.Role {
		case conversationentity.RoleUser:
			senderType = "customer"
		case conversationentity.RoleAssistant:
			if len(message.Payload.ToolCalls) != 0 || message.Payload.ToolCallID != "" ||
				message.Payload.ToolName != "" || message.Payload.IsError {
				continue
			}
			senderType = "ai"
		case conversationentity.RoleTool:
			continue
		default:
			return nil, fmt.Errorf("conversation history message %d has unsupported role %q", index, message.Role)
		}

		content, err := historyTextContent(message.Payload.Content)
		if err != nil {
			return nil, fmt.Errorf("conversation history message %d: %w", index, err)
		}
		historyMessage := pi.Message{
			ContentType: "text",
			Content:     content,
			ID:          message.ID,
			SenderType:  senderType,
		}
		if !message.CreatedAt.IsZero() {
			historyMessage.CreateTime = message.CreatedAt.Format("2006-01-02 15:04:05")
			historyMessage.CreateTS = strconv.FormatInt(message.CreatedAt.UnixMilli(), 10)
		}
		converted = append(converted, historyMessage)
	}
	return converted, nil
}

func historyTextContent(blocks []conversationentity.ContentBlock) (string, error) {
	var content strings.Builder
	for _, block := range blocks {
		if block.Type != conversationentity.ContentTypeText {
			return "", fmt.Errorf("unsupported content type %q", block.Type)
		}
		content.WriteString(block.Text)
	}
	return content.String(), nil
}

// invocationsToDomain 把 pi 的可信调用映射为台账实体（§10.1）。
// traceID 取当前 conversation.run 的 SpanContext；无效（Telemetry 关闭）
// 时传空串并写 NULL。新 Invocation 显式写入 RequestIndex、Outcome 和
// CostQuality，不依赖数据库默认值；TTFT 只复制 Usage.TTFTMS（NULL/0 语义
// 由指针保持）。
func invocationsToDomain(invocations []pi.ModelInvocation, runID, traceID string) []*conversationentity.ModelInvocation {
	if invocations == nil {
		return nil
	}
	converted := make([]*conversationentity.ModelInvocation, len(invocations))
	for index := range invocations {
		usage := invocations[index].Usage
		item := &conversationentity.ModelInvocation{
			RunID:                              runID,
			Sequence:                           invocations[index].Sequence,
			Phase:                              conversationentity.InvocationPhase(invocations[index].Phase),
			PlatformID:                         usage.PlatformID,
			Model:                              usage.Model,
			InputTokens:                        usage.InputTokens,
			OutputTokens:                       usage.OutputTokens,
			InputPriceUSDPerMillionTokens:      usage.InputPriceUSDPerMillionTokens,
			OutputPriceUSDPerMillionTokens:     usage.OutputPriceUSDPerMillionTokens,
			CostUSD:                            usage.CostUSD,
			LatencyMS:                          usage.LatencyMS,
			Outcome:                            conversationentity.InvocationOutcome(invocations[index].Outcome),
			CostQuality:                        string(usage.CostQuality),
			TTFTMS:                             usage.TTFTMS,
			CacheReadTokens:                    usage.CacheReadTokens,
			CacheWriteTokens:                   usage.CacheWriteTokens,
			ReasoningTokens:                    usage.ReasoningTokens,
			CacheReadPriceUSDPerMillionTokens:  usage.CacheReadPriceUSDPerMillionTokens,
			CacheWritePriceUSDPerMillionTokens: usage.CacheWritePriceUSDPerMillionTokens,
		}
		if traceID != "" {
			item.TraceID = &traceID
		}
		if requestIndex := invocations[index].ProviderRequestIndex; requestIndex > 0 {
			item.ProviderRequestIndex = &requestIndex
		}
		if finishReason := invocations[index].FinishReason; finishReason != "" {
			item.FinishReason = &finishReason
		}
		if item.Outcome == "" {
			item.Outcome = conversationentity.InvocationOutcomeAccepted
		}
		if item.CostQuality == "" {
			item.CostQuality = "estimated"
		}
		converted[index] = item
	}
	return converted
}
