package engine

import (
	"context"

	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/tools"
)

const defaultMaxParallelTools = 4

// Agent is the run contract consumed by the application lifecycle.
type Agent interface {
	Run(ctx context.Context, userPrompt string, reporter Reporter) error
}

// AgentEngine 是微型 OS 的核心驱动。
type AgentEngine struct {
	provider    provider.LLMProvider
	registry    tools.Registry
	scheduler   *ToolScheduler
	composer    *ctxpkg.PromptComposer
	skillLoader *ctxpkg.SkillLoader

	// WorkDir (工作区): 借鉴 OpenClaw 的理念，Agent 必须有一个明确的物理边界
	WorkDir string

	// EnableThinking 控制是否在每轮 Action 前执行无工具的慢思考阶段。
	EnableThinking bool

	// MaxParallelTools 限制单个并发安全波次中同时执行的工具数量；小于等于 0 时串行执行。
	MaxParallelTools int
}

// NewAgentEngine 创建 Agent 引擎，绑定模型、工具注册表、Context 组件和工作区。
func NewAgentEngine(
	p provider.LLMProvider,
	r tools.Registry,
	composer *ctxpkg.PromptComposer,
	skillLoader *ctxpkg.SkillLoader,
	workDir string,
	enableThinking bool,
) *AgentEngine {
	return &AgentEngine{
		provider:         p,
		registry:         r,
		scheduler:        NewToolScheduler(r, defaultMaxParallelTools),
		composer:         composer,
		skillLoader:      skillLoader,
		WorkDir:          workDir,
		EnableThinking:   enableThinking,
		MaxParallelTools: defaultMaxParallelTools,
	}
}
