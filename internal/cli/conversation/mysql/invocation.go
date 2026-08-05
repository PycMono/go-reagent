package mysql

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
)

func encodeInvocation(
	invocation agent.ModelInvocation,
	conversationPK uint64,
	turnVersion uint64,
	runID *string,
) (invocationRow, error) {
	platformID := strings.TrimSpace(invocation.Usage.PlatformID)
	model := strings.TrimSpace(invocation.Usage.Model)
	switch {
	case conversationPK == 0:
		return invocationRow{}, errors.New("mysql conversation: invocation conversation primary key is required")
	case invocation.Sequence == 0:
		return invocationRow{}, errors.New("mysql conversation: invocation sequence must be positive")
	case invocation.Phase != agent.ModelInvocationPhaseThinking && invocation.Phase != agent.ModelInvocationPhaseAction:
		return invocationRow{}, errors.New("mysql conversation: invocation phase must be thinking or action")
	case platformID == "":
		return invocationRow{}, errors.New("mysql conversation: invocation platform ID is required")
	case model == "":
		return invocationRow{}, errors.New("mysql conversation: invocation model is required")
	case invocation.Usage.InputTokens < 0:
		return invocationRow{}, errors.New("mysql conversation: invocation input tokens must be non-negative")
	case invocation.Usage.OutputTokens < 0:
		return invocationRow{}, errors.New("mysql conversation: invocation output tokens must be non-negative")
	case invocation.Usage.LatencyMS < 0:
		return invocationRow{}, errors.New("mysql conversation: invocation latency must be non-negative")
	case invalidDecimal(invocation.Usage.InputPriceUSDPerMillionTokens):
		return invocationRow{}, errors.New("mysql conversation: invocation input price is outside the supported range")
	case invalidDecimal(invocation.Usage.OutputPriceUSDPerMillionTokens):
		return invocationRow{}, errors.New("mysql conversation: invocation output price is outside the supported range")
	case invalidDecimal(invocation.Usage.CostUSD):
		return invocationRow{}, errors.New("mysql conversation: invocation cost is outside the supported range")
	}

	var rowRunID *string
	if runID != nil {
		value := *runID
		rowRunID = &value
	}
	return invocationRow{
		ConversationPK:                 conversationPK,
		TurnVersion:                    turnVersion,
		RunID:                          rowRunID,
		Sequence:                       invocation.Sequence,
		Phase:                          string(invocation.Phase),
		PlatformID:                     platformID,
		Model:                          model,
		InputTokens:                    uint64(invocation.Usage.InputTokens),
		OutputTokens:                   uint64(invocation.Usage.OutputTokens),
		InputPriceUSDPerMillionTokens:  decimal12(invocation.Usage.InputPriceUSDPerMillionTokens),
		OutputPriceUSDPerMillionTokens: decimal12(invocation.Usage.OutputPriceUSDPerMillionTokens),
		CostUSD:                        decimal12(invocation.Usage.CostUSD),
		LatencyMS:                      uint64(invocation.Usage.LatencyMS),
	}, nil
}

func invalidDecimal(value float64) bool {
	return value < 0 || value >= ai.MaxUsageDecimalExclusive || math.IsNaN(value) || math.IsInf(value, 0)
}

func decimal12(value float64) string {
	return strconv.FormatFloat(value, 'f', 12, 64)
}
