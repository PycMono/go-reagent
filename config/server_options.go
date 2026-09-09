// 本文件把 MCPConfig 声明翻译为 pi/mcp 的装配选项：纯字段拷贝，
// transport 构造、环境引用解析与沙箱拉起均在 pi/mcp 中实现。
package config

import (
	"time"

	"github.com/PycMono/go-reagent/pi"
	pimcp "github.com/PycMono/go-reagent/pi/mcp"
)

// PIRuntimeOptions 把 config 中的 Agent 运行策略整体翻译为 pi.Options
// 的配置输入（工作区、平台、压缩、循环护栏、额外 handlers、MCP server
// 声明）；组合根只需在此基础上叠加 group 供数（工具、通知）与能力开关。
func (config *Config) PIRuntimeOptions(workDir string) (pi.Options, error) {
	platform, err := config.CurrentPlatformOptions()
	if err != nil {
		return pi.Options{}, err
	}
	handlers, err := NewExtraToolHandlers(config)
	if err != nil {
		return pi.Options{}, err
	}
	opts := pi.Options{
		WorkDir:       workDir,
		Platform:      platform,
		Compaction:    NewCompactionConfig(config, platform),
		LoopDetection: NewLoopDetectionConfig(config),
		ExtraHandlers: handlers,
		MCPServers:    config.MCP.ServerOptions(),
	}
	if config.Agent.WorkspacePolicy != nil {
		opts.WorkspacePolicy = config.Agent.WorkspacePolicy.PI()
	}
	return opts, nil
}

// ServerOptions 返回全部 enabled server 的装配选项，未启用的跳过；
// ${NAME} 解析与 http/stdio transport 构造由 pi/mcp 完成。
func (config *MCPConfig) ServerOptions() []pimcp.ServerOptions {
	opts := make([]pimcp.ServerOptions, 0, len(config.Servers))
	for _, server := range config.Servers {
		if !server.Enabled {
			continue
		}
		opts = append(opts, pimcp.ServerOptions{
			Name:       server.Name,
			Transport:  server.Transport,
			URL:        server.URL,
			HeaderEnv:  server.HeaderEnv,
			Command:    server.Command,
			Args:       server.Args,
			Env:        server.Env,
			CWD:        server.CWD,
			Timeout:    time.Duration(server.Timeout) * time.Second,
			AllowTools: server.AllowTools,
			ToolPrefix: server.ToolPrefix,
		})
	}
	return opts
}
