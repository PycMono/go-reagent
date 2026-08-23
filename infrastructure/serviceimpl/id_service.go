package serviceimpl

import (
	"github.com/bwmarrin/snowflake"
)

type IDService struct {
	node *snowflake.Node
}

// NewIDService 的 workerID 取值范围 [0,1023] 已由 config.Load 校验，
// snowflake.NewNode 的错误不可达。
func NewIDService(workerID int64) *IDService {
	node, _ := snowflake.NewNode(workerID)
	return &IDService{node: node}
}

func (service *IDService) NextID() string {
	return service.node.Generate().String()
}
