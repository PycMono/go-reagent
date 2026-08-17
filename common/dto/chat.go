package dto

// ListConversationsQuery describes keyset pagination for the conversation list.
type ListConversationsQuery struct {
	Cursor      string `form:"cursor"`
	Limit       int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Keyword     string `form:"keyword" binding:"omitempty,max=255"`
	ProfileCode string `form:"profile_code" binding:"omitempty,max=64"`
}

type CreateConversationDTO struct {
	ProfileCode string `json:"profile_code" binding:"required,max=64"`
}

type RenameConversationDTO struct {
	Name string `json:"name" binding:"required"`
}

// ListMessagesQuery describes keyset pagination for detailed message history.
type ListMessagesQuery struct {
	Cursor string `form:"cursor"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
}

type StartRunDTO struct {
	Content string `json:"content" binding:"required"`
}
