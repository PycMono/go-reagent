package serviceimpl

import (
	"fmt"

	"github.com/bwmarrin/snowflake"
)

type IDService struct {
	node *snowflake.Node
}

func NewIDService(workerID int64) (*IDService, error) {
	node, err := snowflake.NewNode(workerID)
	if err != nil {
		return nil, fmt.Errorf("create snowflake node: %w", err)
	}
	return &IDService{node: node}, nil
}

func (service *IDService) NextID() string {
	return service.node.Generate().String()
}
