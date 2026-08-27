package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/loopdetect"
	"github.com/PycMono/go-reagent/pi/middleware"
	"github.com/jinzhu/configor"
)

// LoadOption 调整 Load 的校验行为；不传时保持服务端默认。
type LoadOption func(*loadOptions)

type loadOptions struct {
	allowProcessCWD bool
}

// WithAllowProcessCWD 放开 agent.workspace_dir 不能等于进程当前目录的限制。
// 该规则保护的是"部署中的服务进程目录"（cwd 放着配置与源码），对 CLI 类入口
// 不成立——用户的工作区恰恰是想让 Agent 操作的项目目录。cmd/server 不传本选项。
func WithAllowProcessCWD() LoadOption {
	return func(options *loadOptions) {
		options.allowProcessCWD = true
	}
}

// Load decodes the existing flattened service configuration.
func Load(path string, opts ...LoadOption) (*Config, error) {
	var options loadOptions
	for _, opt := range opts {
		opt(&options)
	}
	var config Config
	if err := configor.Load(&config, path); err != nil {
		return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
	}
	if err := config.normalizeAndValidate(options); err != nil {
		return nil, fmt.Errorf("加载配置 %s 失败: %w", path, err)
	}
	return &config, nil
}

// NewFromEnvironment loads CONFIG_PATH, defaulting to config.json.
func NewFromEnvironment() (*Config, error) {
	path := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	if path == "" {
		path = "config.json"
	}
	return Load(path)
}

// NewPlatform returns the selected model platform for Pi's Fx graph.
func NewPlatform(config *Config) (providers.Options, error) {
	return config.CurrentPlatformOptions()
}

// NewWorkDir 返回 Load 已解析并校验过的 Agent Workspace 绝对路径。
func NewWorkDir(config *Config) pi.WorkDir {
	return pi.WorkDir(config.Agent.WorkspaceDir)
}

// NewCompactionConfig 把业务开关与平台模型容量组合为 Agent 压缩配置。
func NewCompactionConfig(config *Config, platform providers.Options) harness.CompactionConfig {
	return harness.CompactionConfig{
		ContextWindowTokens: platform.ContextWindowTokens,
		EnablePrune:         config.Agent.EnableContextPrune,
	}
}

// NewLoopDetectionConfig 返回 Load 已校验的工具循环护栏装配值；
// 零值即默认启用。
func NewLoopDetectionConfig(config *Config) loopdetect.Config {
	return config.Agent.LoopDetection
}

// NewExtraToolHandlers 把 permissions/tools 配置映射为追加在默认链之后的
// 扩展 Handler，顺序固定为 Permission → Retry → Timeout：权限判定只执行
// 一次，Retry 的后缀即 [Timeout, ExecuteTool]，每次重试获得新的执行期限。
// 全部未配置时返回 nil，链保持纯默认。
func NewExtraToolHandlers(config *Config) (pi.ExtraToolHandlers, error) {
	var handlers pi.ExtraToolHandlers
	permission, err := newPermissionHandler(config)
	if err != nil {
		return nil, err
	}
	if permission != nil {
		handlers = append(handlers, permission)
	}
	if retry := config.Tools.Retry; retry.Attempts > 1 {
		handlers = append(handlers, middleware.Retry(
			retry.Attempts,
			time.Duration(retry.BackoffMs)*time.Millisecond,
			retry.Tools,
		))
	}
	if config.Tools.TimeoutSeconds > 0 {
		handlers = append(handlers, middleware.Timeout(time.Duration(config.Tools.TimeoutSeconds)*time.Second))
	}
	return handlers, nil
}

func newPermissionHandler(config *Config) (middleware.Handler, error) {
	if len(config.Permissions.Rules) == 0 {
		return nil, nil
	}
	rules := make([]middleware.PermissionRule, 0, len(config.Permissions.Rules))
	for _, ruleConfig := range config.Permissions.Rules {
		for _, pattern := range ruleConfig.Patterns {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("permissions 正则 %q 编译失败: %w", pattern, err)
			}
			rules = append(rules, middleware.PermissionRule{
				Tool:    ruleConfig.Tool,
				Pattern: compiled,
				Reason:  ruleConfig.Reason,
			})
		}
	}
	return middleware.Permission(rules), nil
}
