package vo

import (
	"encoding/json"
	"time"

	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
)

type TrainingSessionVO struct {
	ID               string             `json:"id"`
	AgentID          string             `json:"agent_id"`
	ConversationID   string             `json:"conversation_id"`
	BaseVersionID    string             `json:"base_version_id"`
	CandidateHead    string             `json:"candidate_head"`
	CandidatePartial bool               `json:"candidate_partial"`
	Status           training.Status    `json:"status"`
	RowVersion       uint64             `json:"row_version,string"`
	ExpiresAt        time.Time          `json:"expires_at"`
	Validation       json.RawMessage    `json:"validation"`
	Operation        training.Operation `json:"operation"`
	ResultVersionID  *string            `json:"result_version_id"`
}
