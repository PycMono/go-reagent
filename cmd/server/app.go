package main

import (
	"fmt"
	"slices"
	"strings"

	"go.uber.org/fx"

	chattools "github.com/PycMono/go-reagent/application/tool/chat"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/infrastructure"
	agentprofiledriver "github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	"github.com/PycMono/go-reagent/infrastructure/notice"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

// Register 装配完整 Agent 依赖图：config.PIRuntimeOptions 完成全部配置
// 翻译，组合根只叠加 group 供数后一次调用 pi.New；应用层
// （infrastructure/conversation/chatservice）沿用各自的 Register。
var Register = fx.Options(
	notice.Register,
	chattools.Register,
	fx.Provide(
		config.NewFromEnvironment,
		config.NewWorkDir,
		agentprofiledriver.NewCatalog,
	),
	infrastructure.Register,
	platformRegister,
)

// appParams 是组合根收集的全部装配输入：group 汇聚应用层供数，
// 配置翻译由 config.PIRuntimeOptions 完成。
type appParams struct {
	fx.In
	WorkDir   pi.WorkDir
	Config    *config.Config
	ChatTools []ai.Tool     `group:"agent_tools"`
	Notifiers []pi.Notifier `group:"agent_notifiers"`
}

// newApp 叠加应用层供数与服务器能力开关后一次调 pi.New，
// 并把启动/停机序列挂进 fx Lifecycle。
func newApp(lifecycle fx.Lifecycle, params appParams) (*pi.Agent, error) {
	opts, err := serverAgentOptions(params)
	if err != nil {
		return nil, err
	}
	agent, err := pi.New(opts)
	if err != nil {
		return nil, err
	}
	lifecycle.Append(fx.Hook{OnStart: agent.Start, OnStop: agent.Stop})
	return agent, nil
}

func serverAgentOptions(params appParams) (pi.Options, error) {
	opts, err := params.Config.PIRuntimeOptions(string(params.WorkDir))
	if err != nil {
		return pi.Options{}, err
	}
	opts.Tools = params.ChatTools
	opts.Notifiers = params.Notifiers
	opts.AllowExec = true
	opts.AllowWrite = false
	opts.BuiltinSubagent = true
	if params.Config.Agent.WorkspacePolicy == nil {
		opts.WorkspacePolicy = pi.WorkspacePolicy{
			WriteMode:        pi.WorkspaceWriteRestricted,
			WritablePrefixes: []string{".tmp", "scratch"},
		}
		return opts, nil
	}
	if opts.WorkspacePolicy.WriteMode != pi.WorkspaceWriteRestricted || !slices.Contains(opts.WorkspacePolicy.WritablePrefixes, ".tmp") {
		return pi.Options{}, fmt.Errorf("server agent workspace policy must be restricted and include .tmp")
	}
	for _, prefix := range opts.WorkspacePolicy.WritablePrefixes {
		if prefix != ".tmp" && prefix != "scratch" && !strings.HasPrefix(prefix, "scratch/") {
			return pi.Options{}, fmt.Errorf("server agent workspace prefix %q is not allowed", prefix)
		}
	}
	return opts, nil
}

// newAgentRunner 兼容既有消费端（conversation 依赖 pi.Runner 接口）。
func newAgentRunner(agent *pi.Agent) pi.Runner { return agent }
