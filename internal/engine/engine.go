package engine

import (
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/tools"
)

const defaultMaxParallelTools = 4

// AgentEngine 是微型 OS 的核心驱动。
type AgentEngine struct {
	provider    provider.LLMProvider
	registry    tools.Registry
	composer    *ctxpkg.PromptComposer
	skillLoader *ctxpkg.SkillLoader

	// WorkDir (工作区): 借鉴 OpenClaw 的理念，Agent 必须有一个明确的物理边界
	WorkDir string

	// EnableThinking 控制是否在每轮 Action 前执行无工具的慢思考阶段。
	EnableThinking bool

	// MaxParallelTools 限制单个并发安全波次中同时执行的工具数量；小于等于 0 时串行执行。
	MaxParallelTools int
}

// NewAgentEngine 创建 Agent 引擎，绑定模型、工具注册表和工作区，
// 同时初始化 Prompt/Skill 加载器以及默认的工具并发上限。
func NewAgentEngine(
	p provider.LLMProvider,
	r tools.Registry,
	workDir string,
	enableThinking bool,
) *AgentEngine {
	return &AgentEngine{
		provider:         p,
		registry:         r,
		composer:         ctxpkg.NewPromptComposer(workDir),
		skillLoader:      ctxpkg.NewSkillLoader(workDir),
		WorkDir:          workDir,
		EnableThinking:   enableThinking,
		MaxParallelTools: defaultMaxParallelTools,
	}
}
