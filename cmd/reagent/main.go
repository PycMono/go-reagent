package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	agentconfig "github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/tools"
)

func main() {
	workDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("获取工作区失败: %v", err)
	}

	llmProvider, platform, err := providerFromConfig(configurationPath())
	if err != nil {
		log.Fatal(err)
	}
	log.Printf(
		"[Bootstrap] Platform: %s, Protocol: %s, Model: %s\n",
		platform.ID,
		platform.Protocol,
		platform.Model,
	)

	registry, registryCloser, err := registryForWorkDir(workDir)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := registryCloser.Close(); err != nil {
			log.Printf("[Bootstrap] 关闭工具注册表资源失败: %v\n", err)
		}
	}()

	eng := engine.NewAgentEngine(llmProvider, registry, workDir, true)

	prompt := os.Getenv("AGENT_PROMPT")
	if prompt == "" {
		prompt = `请同时调用 read_file 工具读取当前工作区的 README.md、go.mod 和 cmd/reagent/main.go，
然后综合说明这三个文件分别定义了什么内容。`
	}
	if err := eng.Run(context.Background(), prompt); err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}
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

func providerFromConfig(path string) (provider.LLMProvider, agentconfig.PlatformConfig, error) {
	cfg, err := agentconfig.Load(path)
	if err != nil {
		return nil, agentconfig.PlatformConfig{}, err
	}
	platform, err := cfg.Current()
	if err != nil {
		return nil, agentconfig.PlatformConfig{}, err
	}

	llmProvider, err := provider.New(provider.Options{
		Name:     platform.ID,
		Protocol: platform.Protocol,
		BaseURL:  platform.BaseURL,
		APIKey:   platform.APIKey,
		Model:    platform.Model,
	})
	if err != nil {
		return nil, agentconfig.PlatformConfig{}, fmt.Errorf("初始化平台 %q: %w", platform.ID, err)
	}
	return llmProvider, platform, nil
}
