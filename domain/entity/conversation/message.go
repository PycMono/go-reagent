package conversation

import "time"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message records one ordered historical message in a conversation turn.
type Message struct {
	ID             string         `gorm:"column:id;primaryKey;size:32"`
	ConversationID string         `gorm:"column:conversation_id;size:32;not null;uniqueIndex:uq_agent_messages_order,priority:1"`
	TurnVersion    uint64         `gorm:"column:turn_version;not null;uniqueIndex:uq_agent_messages_order,priority:2"`
	Ordinal        uint32         `gorm:"column:ordinal;not null;uniqueIndex:uq_agent_messages_order,priority:3"`
	RunID          string         `gorm:"column:run_id;size:128;not null;default:''"`
	Role           Role           `gorm:"column:role;size:32;not null"`
	Payload        MessagePayload `gorm:"column:payload;type:json;not null"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
}

func (Message) TableName() string { return "agent_messages" }
