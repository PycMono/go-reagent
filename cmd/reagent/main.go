package main

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
)

func main() {
	ctx := context.Background()
	logsdk.SetLogger(newApplicationLogger())

	workDir, err := os.Getwd()
	if err != nil {
		logsdk.Fatal(ctx, "获取工作区失败",
			logsdk.Any("component", "bootstrap"),
			logsdk.Err(err),
		)
	}

	llmProvider, platform, botConfig, err := providerFromConfig(configurationPath())
	if err != nil {
		logsdk.Fatal(ctx, "初始化模型 Provider 失败",
			logsdk.Any("component", "bootstrap"),
			logsdk.Err(err),
		)
	}
	logsdk.Info(ctx, "模型平台初始化成功",
		logsdk.Any("component", "bootstrap"),
		logsdk.Any("platform_id", platform.ID),
		logsdk.Any("protocol", platform.Protocol),
		logsdk.Any("model", platform.Model),
	)

	registry, registryCloser, err := registryForWorkDir(workDir)
	if err != nil {
		logsdk.Fatal(ctx, "初始化工具 Registry 失败",
			logsdk.Any("component", "bootstrap"),
			logsdk.Err(err),
		)
	}
	defer func() {
		if err := registryCloser.Close(); err != nil {
			logsdk.Error(ctx, "关闭工具 Registry 资源失败",
				logsdk.Any("component", "bootstrap"),
				logsdk.Err(err),
			)
		}
	}()

	eng := engine.NewAgentEngine(llmProvider, registry, workDir, true)
	reporter, err := reporterFromConfig(botConfig)
	if err != nil {
		logsdk.Fatal(ctx, "初始化 Reporter 失败",
			logsdk.Any("component", "bootstrap"),
			logsdk.Err(err),
		)
	}

	prompt := os.Getenv("AGENT_PROMPT")
	if prompt == "" {
		prompt = `我当前目录下有 a.txt、b.txt、c.txt 三个文件。
为了节省时间，请你在同一个 Action 中同时调用三次 read_file，分别读取这三个文件，
并将它们的内容综合起来，告诉我它们分别记录了什么领域的信息。
Thinking 阶段只能制定计划，不能假装已经读取文件或编造文件内容。
获得三个文件的真实内容后，下一轮 Action 的对外回复必须完整列出 a.txt、b.txt、c.txt 的内容和领域总结；
即使 Thinking 已经完成分析，也不能只回复确认、致谢或其他简短客套话。`
	}
	if err := eng.Run(ctx, prompt, reporter); err != nil {
		logsdk.Fatal(ctx, "Agent 引擎运行失败",
			logsdk.Any("component", "bootstrap"),
			logsdk.Err(err),
		)
	}
}

func newApplicationLogger() logsdk.Logger {
	return logsdk.NewLogrus(logsdk.Options{
		LogFormat: "json",
		Module:    "go-reagent",
	})
}

func registryForWorkDir(workDir string) (tools.Registry, io.Closer, error) {
	readFileTool, err := tools.NewReadFileTool(workDir)
	if err != nil {
		return nil, nil, fmt.Errorf("初始化 read_file 工具失败: %w", err)
	}
	editFileTool, err := tools.NewEditFileTool(workDir)
	if err != nil {
		_ = readFileTool.Close()
		return nil, nil, fmt.Errorf("初始化 edit_file 工具失败: %w", err)
	}
	closer := toolClosers{readFileTool, editFileTool}

	registry := tools.NewRegistry()
	if err := registry.Register(readFileTool); err != nil {
		_ = closer.Close()
		return nil, nil, fmt.Errorf("挂载 read_file 工具失败: %w", err)
	}
	if err := registry.Register(editFileTool); err != nil {
		_ = closer.Close()
		return nil, nil, fmt.Errorf("挂载 edit_file 工具失败: %w", err)
	}
	return registry, closer, nil
}

type toolClosers []io.Closer

func (closers toolClosers) Close() error {
	var closeErrors []error
	for index := len(closers) - 1; index >= 0; index-- {
		closeErrors = append(closeErrors, closers[index].Close())
	}
	return errors.Join(closeErrors...)
}

func configurationPath() string {
	path := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	if path == "" {
		return "config.json"
	}
	return path
}

func providerFromConfig(path string) (provider.LLMProvider, agentconfig.PlatformConfig, agentconfig.BotConfig, error) {
	cfg, err := agentconfig.Load(path)
	if err != nil {
		return nil, agentconfig.PlatformConfig{}, agentconfig.BotConfig{}, err
	}
	platform, err := cfg.Current()
	if err != nil {
		return nil, agentconfig.PlatformConfig{}, agentconfig.BotConfig{}, err
	}

	llmProvider, err := provider.New(provider.Options{
		Name:     platform.ID,
		Protocol: platform.Protocol,
		BaseURL:  platform.BaseURL,
		APIKey:   platform.APIKey,
		Model:    platform.Model,
	})
	if err != nil {
		return nil, agentconfig.PlatformConfig{}, agentconfig.BotConfig{}, fmt.Errorf("初始化平台 %q: %w", platform.ID, err)
	}
	return llmProvider, platform, cfg.Bot, nil
}

func reporterFromConfig(botConfig agentconfig.BotConfig) (engine.Reporter, error) {
	terminalReporter := engine.NewTerminalReporter()
	if botConfig.WeCom.WebhookURL == "" {
		return terminalReporter, nil
	}
	weComReporter, err := dispatch.NewWeComReporter(botConfig.WeCom.WebhookURL, nil)
	if err != nil {
		return nil, fmt.Errorf("初始化企业微信群 Reporter: %w", err)
	}
	return engine.NewMultiReporter(terminalReporter, weComReporter), nil
}
