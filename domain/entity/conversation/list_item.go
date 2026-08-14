package conversation

// ListItem combines persisted conversation metadata with derived list data.
type ListItem struct {
	Conversation *Conversation
	MessageTotal int64
}
