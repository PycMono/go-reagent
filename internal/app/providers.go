package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	logsdk "github.com/PycMono/go-logger-sdk"
	agentconfig "github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/dispatch"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/tools"
	"go.uber.org/fx"
)

// WorkDir is the Agent workspace path injected through Fx.
type WorkDir string

// Prompt is the one-shot task injected into AgentRunner.
type Prompt string

// Agent is the run contract consumed by AgentRunner.
type Agent interface {
	Run(ctx context.Context, userPrompt string, reporter engine.Reporter) error
}

// NewConfig loads the process configuration selected by CONFIG_PATH.
func NewConfig() (*agentconfig.Config, error) {
	return agentconfig.Load(configurationPath())
}

// NewWorkDir resolves the current process directory as the Agent workspace.
func NewWorkDir() (WorkDir, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取工作区失败: %w", err)
	}
	return WorkDir(workDir), nil
}

// NewLLMProvider creates the currently selected model provider.
func NewLLMProvider(cfg *agentconfig.Config) (provider.LLMProvider, error) {
	if cfg == nil {
		return nil, errors.New("初始化模型 Provider: 配置不能为空")
	}
	platform, err := cfg.Current()
	if err != nil {
		return nil, err
	}
	llmProvider, err := provider.New(provider.Options{
		Name:     platform.ID,
		Protocol: platform.Protocol,
		BaseURL:  platform.BaseURL,
		APIKey:   platform.APIKey,
		Model:    platform.Model,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化平台 %q: %w", platform.ID, err)
	}
	logsdk.Info(context.Background(), "模型平台初始化成功",
		logsdk.Any("component", "bootstrap"),
		logsdk.Any("platform_id", platform.ID),
		logsdk.Any("protocol", platform.Protocol),
		logsdk.Any("model", platform.Model),
	)
	return llmProvider, nil
}

// NewRegistry creates workspace tools and binds their resources to Fx lifecycle.
func NewRegistry(lifecycle fx.Lifecycle, workDir WorkDir) (tools.Registry, error) {
	readFileTool, err := tools.NewReadFileTool(string(workDir))
	if err != nil {
		return nil, fmt.Errorf("初始化 read_file 工具失败: %w", err)
	}
	editFileTool, err := tools.NewEditFileTool(string(workDir))
	if err != nil {
		_ = readFileTool.Close()
		return nil, fmt.Errorf("初始化 edit_file 工具失败: %w", err)
	}
	closer := toolClosers{readFileTool, editFileTool}

	registry := tools.NewRegistry()
	if err := registry.Register(readFileTool); err != nil {
		_ = closer.Close()
		return nil, fmt.Errorf("挂载 read_file 工具失败: %w", err)
	}
	if err := registry.Register(editFileTool); err != nil {
		_ = closer.Close()
		return nil, fmt.Errorf("挂载 edit_file 工具失败: %w", err)
	}

	lifecycle.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		if err := closer.Close(); err != nil {
			logsdk.Error(ctx, "关闭工具 Registry 资源失败",
				logsdk.Any("component", "bootstrap"),
				logsdk.Err(err),
			)
			return err
		}
		return nil
	}})
	return registry, nil
}

// NewReporter creates terminal output and optionally adds enterprise WeChat.
func NewReporter(cfg *agentconfig.Config) (engine.Reporter, error) {
	if cfg == nil {
		return nil, errors.New("初始化 Reporter: 配置不能为空")
	}
	terminalReporter := engine.NewTerminalReporter()
	if cfg.Bot.WeCom.WebhookURL == "" {
		return terminalReporter, nil
	}
	weComReporter, err := dispatch.NewWeComReporter(cfg.Bot.WeCom.WebhookURL, nil)
	if err != nil {
		return nil, fmt.Errorf("初始化企业微信群 Reporter: %w", err)
	}
	return engine.NewMultiReporter(terminalReporter, weComReporter), nil
}

// NewAgentEngine wires the model and tools into the core Agent engine.
func NewAgentEngine(
	llmProvider provider.LLMProvider,
	registry tools.Registry,
	workDir WorkDir,
) Agent {
	return engine.NewAgentEngine(llmProvider, registry, string(workDir), true)
}

// NewPrompt returns the process override or the current concurrency demo task.
func NewPrompt() Prompt {
	if prompt := os.Getenv("AGENT_PROMPT"); prompt != "" {
		return Prompt(prompt)
	}
	return Prompt(`我当前目录下有 a.txt、b.txt、c.txt 三个文件。
为了节省时间，请你在同一个 Action 中同时调用三次 read_file，分别读取这三个文件，
并将它们的内容综合起来，告诉我它们分别记录了什么领域的信息。
Thinking 阶段只能制定计划，不能假装已经读取文件或编造文件内容。
获得三个文件的真实内容后，下一轮 Action 的对外回复必须完整列出 a.txt、b.txt、c.txt 的内容和领域总结；
即使 Thinking 已经完成分析，也不能只回复确认、致谢或其他简短客套话。`)
}

func configurationPath() string {
	path := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	if path == "" {
		return "config.json"
	}
	return path
}

type toolClosers []io.Closer

func (closers toolClosers) Close() error {
	var closeErrors []error
	for index := len(closers) - 1; index >= 0; index-- {
		closeErrors = append(closeErrors, closers[index].Close())
	}
	return errors.Join(closeErrors...)
}
