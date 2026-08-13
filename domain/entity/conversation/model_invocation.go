package conversation

import "time"

type InvocationPhase string

const (
	InvocationPhaseThinking InvocationPhase = "thinking"
	InvocationPhaseAction   InvocationPhase = "action"
)

// ModelInvocation records token usage, price, cost, and latency for one model call.
type ModelInvocation struct {
	ID                             string          `gorm:"column:id;primaryKey;size:32"`
	ConversationID                 string          `gorm:"column:conversation_id;size:32;not null;uniqueIndex:uq_agent_model_invocations_turn_sequence,priority:1"`
	TurnVersion                    uint64          `gorm:"column:turn_version;not null;uniqueIndex:uq_agent_model_invocations_turn_sequence,priority:2"`
	RunID                          string          `gorm:"column:run_id;size:128;not null;default:''"`
	Sequence                       uint32          `gorm:"column:sequence;not null;uniqueIndex:uq_agent_model_invocations_turn_sequence,priority:3"`
	Phase                          InvocationPhase `gorm:"column:phase;size:16;not null"`
	PlatformID                     string          `gorm:"column:platform_id;size:128;not null"`
	Model                          string          `gorm:"column:model;size:255;not null"`
	InputTokens                    int64           `gorm:"column:input_tokens;not null"`
	OutputTokens                   int64           `gorm:"column:output_tokens;not null"`
	InputPriceUSDPerMillionTokens  float64         `gorm:"column:input_price_usd_per_million_tokens;type:decimal(20,12);not null"`
	OutputPriceUSDPerMillionTokens float64         `gorm:"column:output_price_usd_per_million_tokens;type:decimal(20,12);not null"`
	CostUSD                        float64         `gorm:"column:cost_usd;type:decimal(20,12);not null"`
	LatencyMS                      int64           `gorm:"column:latency_ms;not null"`
	CreatedAt                      time.Time       `gorm:"column:created_at"`
}

func (ModelInvocation) TableName() string { return "agent_model_invocations" }
