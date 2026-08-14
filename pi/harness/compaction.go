package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

const maxCompactionInputBytes = 32 * 1024

type CompactionPlan struct {
	SummaryMessages   []ai.Message
	PreservedMessages []ai.Message
}

func BuildCompactionPlan(messages []ai.Message) (CompactionPlan, error) {
	systemEnd := 0
	for systemEnd < len(messages) && messages[systemEnd].Role == ai.RoleSystem {
		systemEnd++
	}
	currentStart := -1
	for index := len(messages) - 1; index >= systemEnd; index-- {
		if messages[index].Role == ai.RoleUser {
			currentStart = index
			break
		}
	}
	if currentStart < 0 {
		return CompactionPlan{}, errors.New("compaction requires a current user turn")
	}

	turns := splitCompactionTurns(messages[systemEnd:currentStart])
	selected := make([]ai.Message, 0)
	for index := len(turns) - 1; index >= 0; index-- {
		candidate := make([]ai.Message, 0, len(turns[index])+len(selected))
		candidate = append(candidate, turns[index]...)
		candidate = append(candidate, selected...)
		encoded, err := json.Marshal(candidate)
		if err != nil {
			return CompactionPlan{}, fmt.Errorf("encode compaction history: %w", err)
		}
		if len(encoded) > maxCompactionInputBytes {
			break
		}
		selected = candidate
	}
	if len(selected) == 0 {
		return CompactionPlan{}, errors.New("compaction has no bounded old history")
	}

	preserved := make([]ai.Message, 0, systemEnd+len(messages)-currentStart)
	preserved = append(preserved, messages[:systemEnd]...)
	preserved = append(preserved, messages[currentStart:]...)
	return CompactionPlan{
		SummaryMessages:   append([]ai.Message(nil), selected...),
		PreservedMessages: append([]ai.Message(nil), preserved...),
	}, nil
}

func splitCompactionTurns(messages []ai.Message) [][]ai.Message {
	if len(messages) == 0 {
		return nil
	}
	start := 0
	turns := make([][]ai.Message, 0)
	for index := 1; index < len(messages); index++ {
		if messages[index].Role != ai.RoleUser {
			continue
		}
		turns = append(turns, append([]ai.Message(nil), messages[start:index]...))
		start = index
	}
	turns = append(turns, append([]ai.Message(nil), messages[start:]...))
	return turns
}

func ApplySummary(plan CompactionPlan, summary string) []ai.Message {
	systemEnd := 0
	for systemEnd < len(plan.PreservedMessages) && plan.PreservedMessages[systemEnd].Role == ai.RoleSystem {
		systemEnd++
	}
	result := make([]ai.Message, 0, len(plan.PreservedMessages)+1)
	result = append(result, plan.PreservedMessages[:systemEnd]...)
	result = append(result, ai.Message{
		Role: ai.RoleSystem,
		Content: []ai.ContentBlock{
			ai.TextBlock("# Earlier conversation summary\n" + strings.TrimSpace(summary)),
		},
	})
	result = append(result, plan.PreservedMessages[systemEnd:]...)
	return result
}
