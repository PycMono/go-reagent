package agent

import "time"

type Starter struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

type Presentation struct {
	Icon     string    `json:"icon"`
	Welcome  string    `json:"welcome"`
	Starters []Starter `json:"starters"`
	Order    int       `json:"order"`
}

type Agent struct {
	ID                      string
	TenantID                string
	Name                    string
	Description             string
	Status                  string
	Presentation            Presentation
	TemplateCode            string
	BootstrapKey            *string
	ActiveVersionID         *string
	ActiveTrainingSessionID *string
	RowVersion              uint64
	CreatedBy               string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}
