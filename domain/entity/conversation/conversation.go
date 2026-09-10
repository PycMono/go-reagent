package conversation

import "time"

// Conversation records the identity and write version of one persisted conversation.
type Conversation struct {
	ID               string    `gorm:"column:id;primaryKey;size:32"`
	TenantID         string    `gorm:"column:tenant_id;size:128"`
	UserID           string    `gorm:"column:user_id;size:128;not null;uniqueIndex:uq_agent_conversations_owner,priority:1"`
	ConversationID   string    `gorm:"column:conversation_id;size:128;not null;uniqueIndex:uq_agent_conversations_owner,priority:2"`
	ConversationType string    `gorm:"column:conversation_type;size:16;not null;default:chat"`
	AgentID          string    `gorm:"column:agent_id;size:32"`
	AgentVersionID   string    `gorm:"column:agent_version_id;size:32"`
	FollowLatest     bool      `gorm:"column:follow_latest;not null"`
	Name             string    `gorm:"column:name;size:255;not null"`
	ProfileCode      string    `gorm:"column:profile_code;size:64;not null;default:general"`
	Version          uint64    `gorm:"column:version;not null;default:0"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
}

func (Conversation) TableName() string { return "agent_conversations" }
