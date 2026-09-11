package dto

// ListConversationsQuery describes keyset pagination for the conversation list.
type ListConversationsQuery struct {
	AgentID     string `form:"agent_id" binding:"omitempty,max=32"`
	Cursor      string `form:"cursor"`
	Limit       int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Keyword     string `form:"keyword" binding:"omitempty,max=255"`
	ProfileCode string `form:"profile_code" binding:"omitempty,max=64"`
}

type CreateConversationDTO struct {
	AgentID     string `json:"agent_id" binding:"omitempty,max=32"`
	ProfileCode string `json:"profile_code" binding:"omitempty,max=64"`
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
	// ImageURLs 是可选附加的用户图片地址列表（至多 4 张）；必须是推理服务商
	// 可访问的 http/https URL。
	ImageURLs []string `json:"image_urls" binding:"omitempty,max=4,dive,http_url"`
}
