// Package loopdetect 提供请求级的工具行为循环检测：它只回答“当前工具行为
// 是否呈现重复且无进展的模式”，不拥有 Agent Loop，也不累计 Token、成本或
// 轮次（那是 governor 的职责）。
//
// Detector 是请求内策略对象：每次 Run 创建一个实例，主代理与每个子代理
// 相互独立；方法只在 Loop 的单线程控制流中按 AdmitToolBatch → 工具执行 →
// RecordToolBatchOutcome 的顺序调用，不保证并发安全。
//
// 包依赖方向固定：governor 可以 import 本包以识别 typed Error；本包不得
// import governor。
package loopdetect

import "strings"

// Config 是循环检测的全部外部配置。零值即默认启用；阈值是包内固定常量，
// 不对外开放。
type Config struct {
	// Disabled 显式关闭循环检测，回到无检测的旧行为。
	Disabled bool `json:"disabled" yaml:"disabled" toml:"disabled"`
	// ExcludedTools 使用最终暴露给模型的精确工具名匹配（大小写敏感），
	// 被排除的调用不进入任何历史、提醒或 critical 计数。
	ExcludedTools []string `json:"excluded_tools" yaml:"excluded_tools" toml:"excluded_tools"`
}

// normalize 返回配置的防御性副本：trim、去空、去重，不修改调用方传入的
// Config。业务校验（空白/重复 fail-fast）只在根 config 包执行，SDK 侧不做
// 第二套校验。
func (config Config) normalize() Config {
	if len(config.ExcludedTools) == 0 {
		return Config{Disabled: config.Disabled}
	}
	seen := make(map[string]struct{}, len(config.ExcludedTools))
	excluded := make([]string, 0, len(config.ExcludedTools))
	for _, name := range config.ExcludedTools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		excluded = append(excluded, name)
	}
	return Config{Disabled: config.Disabled, ExcludedTools: excluded}
}
