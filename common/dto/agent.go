package dto

import (
	"encoding/json"
	"github.com/PycMono/go-reagent/domain/entity/agent"
)

type ModelChoice struct {
	ProviderRef string          `json:"provider_ref"`
	ModelID     string          `json:"model_id"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}
type CreateAgentDTO struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	TemplateCode string             `json:"template_code"`
	ModelConfig  ModelChoice        `json:"model_config"`
	Presentation agent.Presentation `json:"presentation"`
}
type PatchAgentDTO struct {
	ExpectedRowVersion uint64              `json:"expected_row_version,string"`
	Name               *string             `json:"name,omitempty"`
	Description        *string             `json:"description,omitempty"`
	Status             *string             `json:"status,omitempty"`
	Presentation       *agent.Presentation `json:"presentation,omitempty"`
}
type ListAgentsQuery struct {
	Keyword         string `form:"keyword"`
	Cursor          string `form:"cursor"`
	Limit           int    `form:"limit"`
	IncludeArchived bool   `form:"include_archived"`
}

type ModelConfigReleaseDTO struct {
	ExpectedRowVersion uint64      `json:"expected_row_version,string"`
	BaseVersionID      string      `json:"base_version_id"`
	ModelConfig        ModelChoice `json:"model_config"`
	ChangeSummary      string      `json:"change_summary"`
	Confirmed          bool        `json:"confirmed"`
}
