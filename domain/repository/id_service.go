package repository

// IIDService generates string IDs for persisted domain entities.
type IIDService interface {
	NextID() string
}
