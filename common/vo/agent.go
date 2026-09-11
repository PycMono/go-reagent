package vo

import (
	"github.com/PycMono/go-reagent/domain/entity/agent"
	"time"
)

type AgentVO struct {
	ID                      string             `json:"id"`
	Name                    string             `json:"name"`
	Description             string             `json:"description"`
	Icon                    string             `json:"icon"`
	Welcome                 string             `json:"welcome"`
	Starters                []agent.Starter    `json:"starters"`
	Presentation            agent.Presentation `json:"presentation"`
	Status                  string             `json:"status"`
	Selectable              bool               `json:"selectable"`
	TemplateCode            string             `json:"template_code"`
	ActiveVersionID         *string            `json:"active_version_id"`
	ActiveTrainingSessionID *string            `json:"active_training_session_id,omitempty"`
	RowVersion              uint64             `json:"row_version,string"`
}
type AgentPageVO struct {
	Items          []*AgentVO `json:"items"`
	NextCursor     string     `json:"next_cursor"`
	DefaultAgentID *string    `json:"default_agent_id"`
}
type AgentTemplateVO struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
}
type AgentVersionVO struct {
	ID                      string    `json:"id"`
	Number                  uint64    `json:"version,string"`
	PublishedBy             string    `json:"published_by"`
	PublishedAt             time.Time `json:"published_at"`
	ChangeSummary           string    `json:"change_summary"`
	SourceTrainingSessionID *string   `json:"source_training_session_id"`
	Active                  bool      `json:"active"`
}
type AgentVersionPageVO struct {
	Items      []AgentVersionVO `json:"items"`
	NextCursor string           `json:"next_cursor"`
}
