package main

// go-reagent CLI：pi 无状态 Agent Core 的终端多轮对话入口。
// 组合根职责：flags → stderr logger → resolveRuntime（两种配置来源统一为
// RuntimeConfig）→ AGENTS.md 预检 → fx 装配 → REPL/-prompt → shutdown 唯一出口。

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	logsdk "github.com/PycMono/go-logger-sdk"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/harness"
)

func main() {
	flags, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n%s", err, usageText)
		os.Exit(exitError)
	}
	// 必须先于任何可能产生日志的步骤，否则启动期日志污染 stdout。
	logsdk.SetLogger(newStderrLogger(os.Stderr, flags.verbose))

	runtime, cfg, err := resolveRuntime(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(exitError)
	}
	if err := preflightWorkspace(runtime.workDir); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(exitError)
	}

	agent, err := buildAgent(runtime, cfg, flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "装配失败: %v\n", err)
		os.Exit(exitError)
	}

	startCtx, startCancel := context.WithTimeout(context.Background(), 15*time.Second)
	err = agent.Start(startCtx)
	startCancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(exitError)
	}

	printBanner(os.Stderr, runtime, cfg, flags)
	code := runSession(agent, os.Stdin, os.Stdout, os.Stderr, sessionOptions{
		prompt:       flags.prompt,
		promptSet:    flags.promptSet,
		historyLimit: flags.historyLimit,
		limits:       runtime.limits,
		verbose:      flags.verbose,
	})
	shutdown(agent, code)
}

// shutdown 是 Start 之后所有路径的唯一出口；os.Exit 不执行 defer，
// cancel 必须显式调用。
func shutdown(agent *pi.Agent, code int) {
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := agent.Stop(stopCtx) // 触发 Workspace/Supervisor 清理与扩展 Close
	stopCancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "清理失败: %v\n", err)
		os.Exit(exitError)
	}
	os.Exit(code)
}

type cliFlags struct {
	dir          string
	prompt       string
	promptSet    bool
	allowWrite   bool
	allowExec    bool
	yolo         bool
	subagent     bool
	verbose      bool
	maxTurns     int
	maxTurnsSet  bool
	historyLimit int
}

const usageText = `用法: go-reagent-cli [flags]
  -dir string        工作区目录（默认：配置文件的 agent.workspace_dir；无配置时为当前目录）
  -prompt string     单轮模式：执行一次任务后退出
  -allow-write       允许 edit/write/apply_patch 工具
  -allow-exec        允许 exec/process 工具（按当前平台固定隔离策略执行）
  -yolo              全开（等价 -allow-write -allow-exec）
  -subagent          挂载子代理（要求配置文件模式）
  注：配置文件模式自动挂载已启用的 MCP 服务器
     （如 exa 的 web_search_exa/web_fetch_exa，需设置其 header_env 引用的环境变量）
  -max-turns int     每轮运行的 turn 上限（0 或未传使用默认 20）
  -history-limit int 历史消息条数上限（默认 100，最小 2）
  -v                 输出 Info 级日志与非文本块标注

无配置文件时从环境变量构造默认配置：
  ANTHROPIC_API_KEY 或 OPENAI_API_KEY（必填其一，anthropic 优先）
  REAGENT_MODEL / REAGENT_BASE_URL（可选）
`

func parseFlags(args []string) (cliFlags, error) {
	var flags cliFlags
	set := flag.NewFlagSet("go-reagent-cli", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&flags.dir, "dir", "", "")
	set.StringVar(&flags.prompt, "prompt", "", "")
	set.BoolVar(&flags.allowWrite, "allow-write", false, "")
	set.BoolVar(&flags.allowExec, "allow-exec", false, "")
	set.BoolVar(&flags.yolo, "yolo", false, "")
	set.BoolVar(&flags.subagent, "subagent", false, "")
	set.BoolVar(&flags.verbose, "v", false, "")
	set.IntVar(&flags.maxTurns, "max-turns", 0, "")
	set.IntVar(&flags.historyLimit, "history-limit", 100, "")
	if err := set.Parse(args); err != nil {
		return flags, err
	}
	set.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "prompt":
			flags.promptSet = true
		case "max-turns":
			flags.maxTurnsSet = true
		}
	})
	if flags.historyLimit < 2 {
		return flags, fmt.Errorf("-history-limit 必须 >= 2，当前 %d", flags.historyLimit)
	}
	if flags.maxTurns < 0 {
		return flags, fmt.Errorf("-max-turns 不能为负数，当前 %d", flags.maxTurns)
	}
	if set.NArg() > 0 {
		return flags, fmt.Errorf("不支持位置参数 %q", set.Arg(0))
	}
	return flags, nil
}

// runtimeConfig 是两种配置来源的统一产出，fx.Supply 进图后装配完全同构。
type runtimeConfig struct {
	options    providers.Options
	workDir    string
	compaction harness.CompactionConfig
	limits     governor.Limits
}

// resolveRuntime 决定配置来源：显式 CONFIG_PATH 加载失败即报错；只有未设置
// CONFIG_PATH 且默认 config.json 不存在时才进入环境变量模式。配置模式返回
// 非 nil 的 *config.Config（fx 图需要，见 buildOptions）。
func resolveRuntime(flags cliFlags) (*runtimeConfig, *config.Config, error) {
	configPath := strings.TrimSpace(os.Getenv("CONFIG_PATH"))
	if configPath != "" {
		return loadConfigMode(configPath, flags)
	}
	if _, err := os.Stat("config.json"); err == nil {
		return loadConfigMode("config.json", flags)
	}
	return loadEnvMode(flags)
}

// loadConfigMode 复用完整 config.Load（要求本机服务配置齐全）。
func loadConfigMode(path string, flags cliFlags) (*runtimeConfig, *config.Config, error) {
	// configor 的 DEBUG/VERBOSE 置位时会直接向 stdout 打印，违反 stdout 纯净
	// 契约：加载前清除，加载后恢复。
	restore := unsetEnv("CONFIGOR_DEBUG_MODE", "CONFIGOR_VERBOSE_MODE")
	defer restore()
	if flags.dir != "" {
		// 注入 configor 覆盖通道，让校验对象就是最终工作区；env 名由
		// config 包 TestLoadConfigWorkspaceDirEnvOverrideName 钉死。
		os.Setenv("CONFIGOR_AGENT_WORKSPACEDIR", flags.dir)
		defer os.Unsetenv("CONFIGOR_AGENT_WORKSPACEDIR")
	}
	cfg, err := config.Load(path, config.WithAllowProcessCWD())
	if err != nil {
		return nil, nil, err
	}
	platform, err := cfg.CurrentPlatformOptions()
	if err != nil {
		return nil, nil, err
	}
	runtime := &runtimeConfig{
		options:    platform,
		workDir:    cfg.Agent.WorkspaceDir, // Load 已解析为绝对路径
		compaction: config.NewCompactionConfig(cfg, platform),
		limits:     cfg.Agent.Limits,
	}
	applyMaxTurns(&runtime.limits, flags)
	return runtime, cfg, nil
}

// loadEnvMode 无配置文件：环境变量构造默认配置，其余内置默认
// （Pricing 零值、压缩关闭、Limits 零值由 governor 回填 DefaultLimits）。
func loadEnvMode(flags cliFlags) (*runtimeConfig, *config.Config, error) {
	if flags.subagent {
		return nil, nil, fmt.Errorf("-subagent 要求配置文件模式（无 config.json 时不可用）")
	}
	options, err := envProviderOptions()
	if err != nil {
		return nil, nil, err
	}
	workDir, err := resolveEnvWorkDir(flags.dir)
	if err != nil {
		return nil, nil, err
	}
	runtime := &runtimeConfig{options: options, workDir: workDir}
	applyMaxTurns(&runtime.limits, flags)
	return runtime, nil, nil
}

var envProtocolDefaults = map[providers.Protocol]struct {
	id      string
	baseURL string
	model   string
}{
	providers.ProtocolAnthropic: {id: "anthropic", baseURL: "https://api.anthropic.com", model: "claude-sonnet-4-6"},
	providers.ProtocolOpenAI:    {id: "openai", baseURL: "https://api.openai.com/v1", model: "gpt-5"},
}

// envProviderOptions 从环境变量构造 platform：ANTHROPIC_API_KEY 优先于
// OPENAI_API_KEY；REAGENT_MODEL/REAGENT_BASE_URL 作用于选中的协议。
func envProviderOptions() (providers.Options, error) {
	var protocol providers.Protocol
	var apiKey string
	if key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); key != "" {
		protocol, apiKey = providers.ProtocolAnthropic, key
	} else if key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); key != "" {
		protocol, apiKey = providers.ProtocolOpenAI, key
	} else {
		return providers.Options{}, fmt.Errorf("未找到模型凭证：请设置 ANTHROPIC_API_KEY 或 OPENAI_API_KEY（或提供配置文件）")
	}
	defaults := envProtocolDefaults[protocol]
	options := providers.Options{
		ID:       defaults.id,
		Protocol: protocol,
		BaseURL:  defaults.baseURL,
		APIKey:   apiKey,
		Model:    defaults.model,
		Pricing:  &providers.Pricing{}, // 零值对象：成本显示 0
	}
	if model := strings.TrimSpace(os.Getenv("REAGENT_MODEL")); model != "" {
		options.Model = model
	}
	if baseURL := strings.TrimSpace(os.Getenv("REAGENT_BASE_URL")); baseURL != "" {
		options.BaseURL = baseURL
	}
	if err := options.NormalizeAndValidate(); err != nil {
		return providers.Options{}, fmt.Errorf("环境变量配置无效: %w", err)
	}
	return options, nil
}

func resolveEnvWorkDir(dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("获取当前目录失败: %w", err)
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("工作区 %q 无效: %w", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("工作区 %q 无效: %w", dir, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("工作区 %q 不存在或不是目录", dir)
	}
	return resolved, nil
}

// applyMaxTurns 应用 -max-turns 覆盖；0/未传保留来源值（零值由 governor
// 回填 DefaultLimits，pi 契约中 Limits 不表达"不限制"）。
func applyMaxTurns(limits *governor.Limits, flags cliFlags) {
	if flags.maxTurnsSet && flags.maxTurns > 0 {
		limits.MaxTurns = flags.maxTurns
	}
}

func unsetEnv(names ...string) func() {
	type saved struct {
		value string
		set   bool
	}
	values := make(map[string]saved, len(names))
	for _, name := range names {
		value, set := os.LookupEnv(name)
		values[name] = saved{value, set}
		os.Unsetenv(name)
	}
	return func() {
		for name, item := range values {
			if item.set {
				os.Setenv(name, item.value)
			} else {
				os.Unsetenv(name)
			}
		}
	}
}

// preflightWorkspace 复刻 pi 对 AGENTS.md 的契约（pi/harness/prompt.go），
// 把"首轮 Run 才失败"提前为启动期可操作错误。
func preflightWorkspace(workDir string) error {
	path := filepath.Join(workDir, "AGENTS.md")
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("工作区 %s 缺少 AGENTS.md：Agent 指令文件必须存在于工作区根目录", workDir)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("工作区 %s 的 AGENTS.md 无效：必须是普通文件（不能是符号链接或目录）", workDir)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return fmt.Errorf("工作区 %s 的 AGENTS.md 无效：必须是合法 UTF-8 文本且不含 NUL 字节", workDir)
	}
	if strings.TrimSpace(string(content)) == "" {
		return fmt.Errorf("工作区 %s 的 AGENTS.md 无效：内容不能为空", workDir)
	}
	return nil
}

// buildAgent 用 pi.New 完成一次装配：平台沙箱 Runner 经探针校验，
// MCP 随配置模式默认挂载（无启用项时为空扩展），-subagent 只负责
// 追加子代理；配置中的循环护栏与 handlers 策略不得静默忽略。
func buildAgent(runtime *runtimeConfig, cfg *config.Config, flags cliFlags) (*pi.Agent, error) {
	options, err := pi.Options{}, error(nil)
	if cfg != nil {
		options, err = cfg.PIRuntimeOptions(runtime.workDir)
		if err != nil {
			return nil, err
		}
		options.BuiltinSubagent = flags.subagent
	} else {
		options = pi.Options{
			WorkDir:    runtime.workDir,
			Platform:   runtime.options,
			Compaction: runtime.compaction,
		}
	}
	options.AllowWrite = flags.allowWrite || flags.yolo
	options.AllowExec = flags.allowExec || flags.yolo
	return pi.New(options)
}

func printBanner(w io.Writer, runtime *runtimeConfig, cfg *config.Config, flags cliFlags) {
	write := flags.allowWrite || flags.yolo
	exec := flags.allowExec || flags.yolo
	tiers := []string{"read"}
	if write {
		tiers = append(tiers, "write")
	}
	if exec {
		tiers = append(tiers, "exec")
	}
	fmt.Fprintf(w, "go-reagent CLI | 工作区: %s\n授权档位: %s\n", runtime.workDir, strings.Join(tiers, " + "))
	if cfg != nil {
		names := make([]string, 0, len(cfg.MCP.Servers))
		for _, server := range cfg.MCP.Servers {
			if server.Enabled {
				names = append(names, server.Name)
			}
		}
		if len(names) == 0 {
			fmt.Fprintln(w, "MCP: 无启用的服务器")
		} else {
			fmt.Fprintf(w, "MCP: %s\n", strings.Join(names, ", "))
		}
	}
	if exec {
		fmt.Fprintln(w, "exec 隔离：macOS 使用 Seatbelt，Linux 使用 Bubblewrap，Windows 使用 Host；网络固定允许。")
	}
}
