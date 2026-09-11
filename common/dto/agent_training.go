package dto

type CreateTrainingDTO struct {
	ExpectedRowVersion uint64 `json:"expected_row_version,string"`
}
type TrainingMutationDTO struct {
	ExpectedRowVersion uint64 `json:"expected_row_version,string"`
	RequestID          string `json:"request_id"`
}
type TrainingRunDTO struct {
	TrainingMutationDTO
	RunID     string   `json:"run_id"`
	Content   string   `json:"content"`
	ImageURLs []string `json:"image_urls,omitempty"`
}
type TrainingConfigDTO struct {
	TrainingMutationDTO
	ModelConfig ModelChoice `json:"model_config"`
}
type TrainingPublishDTO struct {
	TrainingMutationDTO
	HumanConfirmed bool   `json:"human_confirmed"`
	ChangeSummary  string `json:"change_summary"`
}

type TrainingPreviewMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type TrainingPreviewDTO struct {
	Content string                   `json:"content"`
	History []TrainingPreviewMessage `json:"history,omitempty"`
}
