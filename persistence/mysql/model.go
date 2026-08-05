package mysql

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

type conversationRow struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	UserID         string    `gorm:"column:user_id"`
	ConversationID string    `gorm:"column:conversation_id"`
	Version        uint64    `gorm:"column:version"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (conversationRow) TableName() string { return "agent_conversations" }

type messageRow struct {
	ID             uint64      `gorm:"column:id;primaryKey;autoIncrement"`
	ConversationPK uint64      `gorm:"column:conversation_pk"`
	TurnVersion    uint64      `gorm:"column:turn_version"`
	Ordinal        uint32      `gorm:"column:ordinal"`
	RunID          *string     `gorm:"column:run_id"`
	Role           string      `gorm:"column:role"`
	Payload        jsonPayload `gorm:"column:payload;type:json"`
	CreatedAt      time.Time   `gorm:"column:created_at"`
}

func (messageRow) TableName() string { return "agent_messages" }

type invocationRow struct {
	ID                             uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ConversationPK                 uint64    `gorm:"column:conversation_pk"`
	TurnVersion                    uint64    `gorm:"column:turn_version"`
	RunID                          *string   `gorm:"column:run_id"`
	Sequence                       uint32    `gorm:"column:sequence"`
	Phase                          string    `gorm:"column:phase"`
	PlatformID                     string    `gorm:"column:platform_id"`
	Model                          string    `gorm:"column:model"`
	InputTokens                    uint64    `gorm:"column:input_tokens"`
	OutputTokens                   uint64    `gorm:"column:output_tokens"`
	InputPriceUSDPerMillionTokens  string    `gorm:"column:input_price_usd_per_million_tokens"`
	OutputPriceUSDPerMillionTokens string    `gorm:"column:output_price_usd_per_million_tokens"`
	CostUSD                        string    `gorm:"column:cost_usd"`
	LatencyMS                      uint64    `gorm:"column:latency_ms"`
	CreatedAt                      time.Time `gorm:"column:created_at"`
}

func (invocationRow) TableName() string { return "agent_model_invocations" }

type jsonPayload []byte

func (p jsonPayload) Value() (driver.Value, error) {
	if p == nil {
		return nil, errors.New("mysql conversation: JSON payload is required")
	}
	return append([]byte(nil), p...), nil
}

func (p *jsonPayload) Scan(source any) error {
	if p == nil {
		return errors.New("mysql conversation: JSON payload receiver is nil")
	}
	switch value := source.(type) {
	case []byte:
		*p = append((*p)[:0], value...)
	case string:
		*p = append((*p)[:0], value...)
	default:
		return fmt.Errorf("mysql conversation: unsupported JSON payload type %T", source)
	}
	return nil
}
