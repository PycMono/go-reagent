package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/governor"
)

func TestParseFlagsValidation(t *testing.T) {
	if _, err := parseFlags([]string{"-history-limit", "1"}); err == nil {
		t.Fatal("-history-limit < 2 应报错")
	}
	if _, err := parseFlags([]string{"-max-turns", "-1"}); err == nil {
		t.Fatal("负 -max-turns 应报错")
	}
	if _, err := parseFlags([]string{"positional"}); err == nil {
		t.Fatal("位置参数应报错")
	}

	flags, err := parseFlags([]string{"-prompt", "", "-max-turns", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.promptSet {
		t.Fatal("显式 -prompt 空串应与未传区分（promptSet=true）")
	}
	// -max-turns 0 显式传入与未传语义相同：不覆盖来源值。
	limits := governor.Limits{MaxTurns: 7}
	applyMaxTurns(&limits, flags)
	if limits.MaxTurns != 7 {
		t.Fatalf("-max-turns 0 不应覆盖, got %d", limits.MaxTurns)
	}
	flags, err = parseFlags([]string{"-max-turns", "30"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.maxTurnsSet || flags.maxTurns != 30 {
		t.Fatalf("flags = %+v", flags)
	}
}

func TestEnvProviderOptions(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		if _, err := envProviderOptions(); err == nil {
			t.Fatal("缺 key 应报错")
		}
	})
	t.Run("anthropic preferred", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "sk-ant")
		t.Setenv("OPENAI_API_KEY", "sk-oai")
		options, err := envProviderOptions()
		if err != nil {
			t.Fatal(err)
		}
		if options.Protocol != providers.ProtocolAnthropic || options.ID != "anthropic" ||
			options.BaseURL != "https://api.anthropic.com" || options.Model != "claude-sonnet-4-6" {
			t.Fatalf("options = %+v", options)
		}
		if options.Pricing == nil {
			t.Fatal("Pricing 必须是零值对象而非 nil")
		}
	})
	t.Run("openai with overrides", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-oai")
		t.Setenv("REAGENT_MODEL", "gpt-5-mini")
		t.Setenv("REAGENT_BASE_URL", "https://proxy.test/v1")
		options, err := envProviderOptions()
		if err != nil {
			t.Fatal(err)
		}
		if options.Model != "gpt-5-mini" || options.BaseURL != "https://proxy.test/v1" {
			t.Fatalf("options = %+v", options)
		}
	})
}

func TestResolveRuntimeEnvMode(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant")
	workDir := t.TempDir()
	flags := cliFlags{dir: workDir, historyLimit: 100}

	runtime, cfg, err := resolveRuntime(flags)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatal("env 模式不应返回 *config.Config")
	}
	if runtime.workDir != workDir && !strings.HasSuffix(runtime.workDir, filepath.Base(workDir)) {
		t.Fatalf("workDir = %q", runtime.workDir)
	}
	// Limits 零值：由 governor 回填 DefaultLimits，CLI 不填默认。
	if runtime.limits != (governor.Limits{}) {
		t.Fatalf("env 模式 Limits 应为零值, got %+v", runtime.limits)
	}
}

func TestResolveRuntimeEnvModeRejectsSubagent(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant")
	if _, _, err := resolveRuntime(cliFlags{subagent: true, historyLimit: 100}); err == nil {
		t.Fatal("env 模式 -subagent 应报错")
	}
}

func TestResolveRuntimeExplicitConfigPathFailure(t *testing.T) {
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant")
	if _, _, err := resolveRuntime(cliFlags{historyLimit: 100}); err == nil {
		t.Fatal("显式 CONFIG_PATH 加载失败必须报错，不得回退 env 模式")
	}
}

func TestResolveRuntimeConfigMode(t *testing.T) {
	workDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	document := `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
		"agent":{"workspace_dir":"` + workDir + `","limits":{"max_turns":7}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
	}`
	if err := os.WriteFile(configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", configPath)

	runtime, cfg, err := resolveRuntime(cliFlags{historyLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("配置模式必须返回 *config.Config（mcpdriver 依赖）")
	}
	if runtime.options.Model != "m" || runtime.limits.MaxTurns != 7 {
		t.Fatalf("runtime = %+v", runtime)
	}

	// -max-turns 显式正数覆盖配置值。
	runtime, _, err = resolveRuntime(cliFlags{historyLimit: 100, maxTurns: 30, maxTurnsSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.limits.MaxTurns != 30 {
		t.Fatalf("-max-turns 覆盖失败: %+v", runtime.limits)
	}
}

func TestPreflightWorkspace(t *testing.T) {
	write := func(dir, name string, content []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("missing", func(t *testing.T) {
		if err := preflightWorkspace(t.TempDir()); err == nil {
			t.Fatal("缺少 AGENTS.md 应报错")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := write(dir, "real.md", []byte("指令"))
		if err := os.Symlink(target, filepath.Join(dir, "AGENTS.md")); err != nil {
			t.Fatal(err)
		}
		if err := preflightWorkspace(dir); err == nil {
			t.Fatal("符号链接应拒绝")
		}
	})
	t.Run("invalid utf8", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "AGENTS.md", []byte{0xff, 0xfe})
		if err := preflightWorkspace(dir); err == nil {
			t.Fatal("非 UTF-8 应拒绝")
		}
	})
	t.Run("nul byte", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "AGENTS.md", []byte{'a', 0, 'b'})
		if err := preflightWorkspace(dir); err == nil {
			t.Fatal("含 NUL 应拒绝")
		}
	})
	t.Run("empty", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "AGENTS.md", []byte("   \n "))
		if err := preflightWorkspace(dir); err == nil {
			t.Fatal("空内容应拒绝")
		}
	})
	t.Run("valid", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "AGENTS.md", []byte("你是助手"))
		if err := preflightWorkspace(dir); err != nil {
			t.Fatal(err)
		}
	})
}

// buildToolsetAgent 用 fake platform 调 pi.New，断言授权档位最终工具
// 集合。构造即可断言，无需 Start（本用例无 MCP）。
func buildToolsetAgent(t *testing.T, flags cliFlags) *pi.Agent {
	t.Helper()
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("指令"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &runtimeConfig{
		options: providers.Options{
			ID: "fake", Protocol: providers.ProtocolOpenAI,
			BaseURL: "https://fake.test/", APIKey: "k", Model: "m",
			Pricing: &providers.Pricing{},
		},
		workDir: workDir,
	}
	agent, err := pi.New(pi.Options{
		WorkDir:         runtime.workDir,
		Platform:        runtime.options,
		AllowWrite:      flags.allowWrite || flags.yolo,
		AllowExec:       flags.allowExec || flags.yolo,
		WorkspacePolicy: pi.WorkspacePolicy{WriteMode: pi.WorkspaceWriteAll},
	})
	if err != nil {
		t.Fatalf("pi.New 失败: %v", err)
	}
	return agent
}

func toolNames(agent *pi.Agent) map[string]bool {
	names := make(map[string]bool)
	for _, definition := range agent.ToolDefinitions() {
		names[definition.Name] = true
	}
	return names
}

func TestToolsetTiers(t *testing.T) {
	writeTools := []string{"edit", "write", "apply_patch"}
	execTools := []string{"exec", "process"}

	tests := []struct {
		name    string
		flags   cliFlags
		want    []string
		notWant []string
	}{
		{name: "default read-only", flags: cliFlags{}, want: []string{"read"}, notWant: append(append([]string{}, writeTools...), execTools...)},
		{name: "write", flags: cliFlags{allowWrite: true}, want: append([]string{"read"}, writeTools...), notWant: execTools},
		{name: "exec", flags: cliFlags{allowExec: true}, want: append([]string{"read"}, execTools...), notWant: writeTools},
		{name: "write+exec", flags: cliFlags{allowWrite: true, allowExec: true}, want: append(append([]string{"read"}, writeTools...), execTools...)},
		{name: "yolo", flags: cliFlags{yolo: true}, want: append(append([]string{"read"}, writeTools...), execTools...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := toolNames(buildToolsetAgent(t, tt.flags))
			for _, want := range tt.want {
				if !names[want] {
					t.Errorf("缺少工具 %q，当前: %v", want, names)
				}
			}
			for _, notWant := range tt.notWant {
				if names[notWant] {
					t.Errorf("不应有工具 %q，当前: %v", notWant, names)
				}
			}
		})
	}
}

func TestCLIWorkspacePolicyDefaultsToAllOnlyWhenUnset(t *testing.T) {
	for _, tt := range []struct {
		name   string
		policy *config.WorkspacePolicyConfig
		flags  cliFlags
		mode   pi.WorkspaceWriteMode
		prefix []string
	}{
		{name: "unset", mode: pi.WorkspaceWriteAll},
		{name: "restricted write", policy: &config.WorkspacePolicyConfig{WriteMode: "restricted", WritablePrefixes: []string{"scratch"}}, flags: cliFlags{allowWrite: true}, mode: pi.WorkspaceWriteRestricted, prefix: []string{"scratch"}},
		{name: "restricted exec", policy: &config.WorkspacePolicyConfig{WriteMode: "restricted", WritablePrefixes: []string{".tmp", "scratch"}}, flags: cliFlags{allowExec: true}, mode: pi.WorkspaceWriteRestricted, prefix: []string{".tmp", "scratch"}},
		{name: "restricted yolo", policy: &config.WorkspacePolicyConfig{WriteMode: "restricted", WritablePrefixes: []string{".tmp", "scratch"}}, flags: cliFlags{yolo: true}, mode: pi.WorkspaceWriteRestricted, prefix: []string{".tmp", "scratch"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("instructions"), 0o600); err != nil {
				t.Fatal(err)
			}
			runtime := &runtimeConfig{workDir: workDir, options: providers.Options{ID: "fake", Protocol: providers.ProtocolOpenAI, BaseURL: "https://fake.test/", APIKey: "k", Model: "m", Pricing: &providers.Pricing{}}}
			var cfg *config.Config
			if tt.policy != nil {
				cfg = &config.Config{CurrentPlatform: "fake", Platforms: []providers.Options{runtime.options}, Agent: config.AgentConfig{WorkspacePolicy: tt.policy}}
			}
			options, err := cliAgentOptions(runtime, cfg, tt.flags)
			if err != nil {
				t.Fatal(err)
			}
			got := options.WorkspacePolicy
			if got.WriteMode != tt.mode || !reflect.DeepEqual(got.WritablePrefixes, tt.prefix) {
				t.Fatalf("policy = %+v, want mode %q prefixes %v", got, tt.mode, tt.prefix)
			}
			agent, err := pi.New(options)
			if err != nil {
				t.Fatal(err)
			}
			names := toolNames(agent)
			if names["write"] != (tt.flags.allowWrite || tt.flags.yolo) || names["exec"] != (tt.flags.allowExec || tt.flags.yolo) {
				t.Fatalf("tool registration = %v", names)
			}
		})
	}
}
