package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/config"
	agentprofiledriver "github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/skills"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"gopkg.in/yaml.v3"
)

var workspaceMarkdownPathPattern = regexp.MustCompile("`((?:profiles|skills)/[^`]+\\.md)`")

func TestNewChatWorkDirResolvesConfiguredDirectory(t *testing.T) {
	root := t.TempDir()
	got, err := NewChatWorkDir(&config.Config{Agent: config.AgentConfig{WorkspaceDir: "  " + root + "  "}})
	if err != nil {
		t.Fatalf("NewChatWorkDir() error = %v", err)
	}
	gotInfo, err := os.Stat(string(got))
	if err != nil {
		t.Fatalf("stat resolved Workspace: %v", err)
	}
	wantInfo, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat expected Workspace: %v", err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("NewChatWorkDir() = %q, want directory %q", got, root)
	}
}

func TestDefaultChatWorkspacePromptDoesNotLoadRepositoryCodingIdentity(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	workDir, err := NewChatWorkDir(&config.Config{Agent: config.AgentConfig{WorkspaceDir: workspace}})
	if err != nil {
		t.Fatalf("NewChatWorkDir() error = %v", err)
	}
	builder := harness.NewContextBuilder(harness.NewPromptComposer(string(workDir)), string(workDir))
	got, err := builder.Build(context.Background(), harness.ContextRequest{
		Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("你好")}},
	}, []ai.ToolDefinition{{Name: "read"}})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	systemText, err := ai.TextContent(got.Messages[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemText, "通用聊天助手") {
		t.Fatalf("system prompt missing Chat identity: %q", systemText)
	}
	if strings.Contains(systemText, "仓库自带命令行程序") || strings.Contains(systemText, "repository-development") {
		t.Fatalf("system prompt leaked repository Coding identity: %q", systemText)
	}
	wantCatalog := map[string]string{
		"decision-support":     "Use when the user needs a choice, ranking, shortlist, or recommendation.",
		"learning-explanation": "Use when the user asks for a brief explanation or says they do not understand something.",
		"weather-assistance":   "Use when the answer depends on weather, temperature, precipitation, wind, clothing, umbrellas, or outdoor plans.",
		"writing-assistance":   "Use when the user needs a basic email, notice, message, summary, or other text and no more specific writing workflow is available.",
	}
	for name, descriptionFragment := range wantCatalog {
		location := "skills/" + name + "/SKILL.md"
		for _, wanted := range []string{
			"<name>" + name + "</name>",
			"<location>" + location + "</location>",
		} {
			if !strings.Contains(systemText, wanted) {
				t.Fatalf("system prompt missing %q: %q", wanted, systemText)
			}
		}
		if !strings.Contains(systemText, descriptionFragment) {
			t.Fatalf("system prompt missing description fragment %q: %q", descriptionFragment, systemText)
		}
	}
	for _, bodyOnly := range []string{
		"给出带条件的推荐，并说明主要代价",
		"至少提供一个贴合问题的例子",
		"Tool 失败时说明暂时无法获取",
		"信息充分时直接给成稿",
	} {
		if strings.Contains(systemText, bodyOnly) {
			t.Fatalf("system prompt eagerly loaded Skill body %q", bodyOnly)
		}
	}
}

func TestDefaultChatWorkspaceSkillsHaveNoDiagnostics(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	snapshot, err := skills.Discover(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := snapshot.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("Skill diagnostics = %#v", diagnostics)
	}
}

func TestDefaultWorkspaceRoutesPublicInformationThroughExa(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	agents := mustReadWorkspaceFile(t, filepath.Join(workspace, "AGENTS.md"))
	weather := mustReadWorkspaceFile(t, filepath.Join(workspace, "skills", "weather-assistance", "SKILL.md"))
	for _, fragment := range []string{"web_search_exa", "web_fetch_exa", "不回退到其他公网数据源"} {
		if !strings.Contains(agents+weather, fragment) {
			t.Errorf("Exa-only Workspace contract missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"`get_weather`", "Open-Meteo"} {
		if strings.Contains(weather, forbidden) {
			t.Errorf("weather Skill retains forbidden dependency %q", forbidden)
		}
	}
}

func TestDefaultChatWorkspaceKeepsUserLanguageAndDefaultsToPlainText(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	agents := mustReadWorkspaceFile(t, filepath.Join(workspace, "AGENTS.md"))
	for _, fragment := range []string{
		"任何一条 Assistant 消息",
		"以最新一条用户消息的主要语言作为本轮回复语言",
		"Tool、网页、引用资料或专有名词使用其他语言时，不得因此切换回复语言",
		"用户未明确指定格式时，默认使用自然段纯文本",
		"不主动使用 Markdown 标题、加粗、列表、引用或表格",
		"不得使用 `#`、`**`、`-`、`*`、`>` 或 `1.` 等 Markdown 标记",
		"需要表达顺序时，在自然段中使用“第一，”“第二，”等普通文本",
		"发送前必须自检",
	} {
		if !strings.Contains(agents, fragment) {
			t.Errorf("default response contract missing %q", fragment)
		}
	}
	weather := mustReadWorkspaceFile(t, filepath.Join(workspace, "skills", "weather-assistance", "SKILL.md"))
	if !strings.Contains(weather, "遵守根 AGENTS.md 的语言与纯文本规则") {
		t.Error("weather output contract does not reinforce the global response policy")
	}
	workDir, err := NewChatWorkDir(&config.Config{Agent: config.AgentConfig{WorkspaceDir: workspace}})
	if err != nil {
		t.Fatal(err)
	}
	contextBuilder := harness.NewContextBuilder(harness.NewPromptComposer(string(workDir)), string(workDir))
	prepared, err := contextBuilder.Build(context.Background(), harness.ContextRequest{
		Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("成都明天天气怎么样？")}},
	}, []ai.ToolDefinition{{Name: "read"}})
	if err != nil {
		t.Fatal(err)
	}
	systemText, err := ai.TextContent(prepared.Messages[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(systemText, "## 硬性回复契约") < strings.Index(systemText, "</available_skills>") {
		t.Error("Workspace response contract must appear after the Skill catalog")
	}
}

func mustReadWorkspaceFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestDefaultChatWorkspaceDiscoversOnlyGeneralChatSkills(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	snapshot, err := skills.Discover(workspace)
	if err != nil {
		t.Fatal(err)
	}
	summaries := snapshot.Skills()
	names := make([]string, len(summaries))
	for i, summary := range summaries {
		names[i] = summary.Name
	}
	want := []string{"decision-support", "learning-explanation", "weather-assistance", "writing-assistance"}
	if !slices.Equal(names, want) {
		t.Fatalf("skills = %v, want %v", names, want)
	}
	if diagnostics := snapshot.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, summary := range summaries {
		if summary.Location != "skills/"+summary.Name+"/SKILL.md" || summary.Source != skills.SourceWorkspace {
			t.Fatalf("summary = %#v", summary)
		}
	}
}

func TestDefaultChatWorkspaceLoadsAllAgentProfiles(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	catalog, err := agentprofiledriver.NewCatalog(pi.WorkDir(workspace))
	if err != nil {
		t.Fatal(err)
	}
	profiles := catalog.List()
	codes := make([]string, len(profiles))
	for index, profile := range profiles {
		codes[index] = profile.Code
		if strings.TrimSpace(profile.Instructions) == "" {
			t.Fatalf("profile %q has empty instructions", profile.Code)
		}
	}
	want := []string{"general", "writing", "learning", "health", "legal", "automotive", "workplace", "parenting"}
	if !slices.Equal(codes, want) || catalog.DefaultCode() != "general" {
		t.Fatalf("profiles/default = %v / %q, want %v / general", codes, catalog.DefaultCode(), want)
	}
	wantSkills := map[string][]string{
		"general":    {},
		"writing":    {"long-form-structure", "rewrite-and-polish", "social-content"},
		"learning":   {"concept-explanation", "practice-design", "study-planning"},
		"health":     {"care-visit-preparation", "health-report-explanation", "symptom-organizing"},
		"legal":      {"contract-clause-analysis", "facts-and-evidence-organizing", "legal-consultation-preparation"},
		"automotive": {"maintenance-planning", "vehicle-comparison", "vehicle-symptom-triage"},
		"workplace":  {"difficult-workplace-conversation", "status-reporting", "work-message-writing"},
		"parenting":  {"child-development-guidance", "parent-child-communication", "routine-building"},
	}
	for _, code := range want {
		profile, found := catalog.Find(code)
		if !found {
			t.Fatalf("profile %q = %#v, found=%v", code, profile, found)
		}
		gotSkills := make([]string, len(profile.Skills))
		for index, skill := range profile.Skills {
			gotSkills[index] = skill.Name
			if !strings.HasPrefix(skill.Location, "profiles/"+code+"/skills/") {
				t.Fatalf("profile %q Skill location = %q", code, skill.Location)
			}
		}
		if !slices.Equal(gotSkills, wantSkills[code]) {
			t.Fatalf("profile %q Skills = %v, want %v", code, gotSkills, wantSkills[code])
		}
	}
}

func TestAgentProfileSkillContracts(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	catalog, err := agentprofiledriver.NewCatalog(pi.WorkDir(workspace))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := skills.Discover(workspace)
	if err != nil {
		t.Fatal(err)
	}

	type skillContract struct {
		name, description, location string
	}
	contracts := make([]skillContract, 0, 25)
	for _, summary := range snapshot.Skills() {
		contracts = append(contracts, skillContract{
			name: summary.Name, description: summary.Description, location: summary.Location,
		})
	}
	for _, profile := range catalog.List() {
		for _, skill := range profile.Skills {
			contracts = append(contracts, skillContract{
				name: skill.Name, description: skill.Description, location: skill.Location,
			})
		}
	}
	if len(contracts) != 25 {
		t.Fatalf("Skill contract count = %d, want 25", len(contracts))
	}

	requiredDescriptionParts := []string{"Use when", "Triggers", "Do not use when"}
	requiredHeadings := []string{
		"## 目标", "## 必要输入", "## 硬门禁", "## 执行流程", "## 输出契约",
		"## References 与 Templates", "## 边界", "## 示例", "## 常见错误",
	}
	for _, contract := range contracts {
		t.Run(contract.name, func(t *testing.T) {
			for _, part := range requiredDescriptionParts {
				if !strings.Contains(contract.description, part) {
					t.Errorf("description missing %q: %q", part, contract.description)
				}
			}
			content, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(contract.location)))
			if readErr != nil {
				t.Fatal(readErr)
			}
			text := string(content)
			if !strings.Contains(text, "description: >-") {
				t.Error("description must use folded YAML form")
			}
			for _, heading := range requiredHeadings {
				if !strings.Contains(text, heading) {
					t.Errorf("body missing heading %q", heading)
				}
			}
			for _, match := range workspaceMarkdownPathPattern.FindAllStringSubmatch(text, -1) {
				info, statErr := os.Stat(filepath.Join(workspace, filepath.FromSlash(match[1])))
				if statErr != nil || !info.Mode().IsRegular() {
					t.Errorf("referenced file %q is not a regular Workspace file: %v", match[1], statErr)
				}
			}
		})
	}
}

func TestAgentProfileSkillRoutingCorpus(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	catalog, err := agentprofiledriver.NewCatalog(pi.WorkDir(workspace))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := skills.Discover(workspace)
	if err != nil {
		t.Fatal(err)
	}

	var corpus struct {
		Cases []struct {
			Name           string   `yaml:"name"`
			Profile        string   `yaml:"profile"`
			Prompt         string   `yaml:"prompt"`
			ExpectedSkills []string `yaml:"expected_skills"`
			ExcludedSkills []string `yaml:"excluded_skills"`
		} `yaml:"cases"`
	}
	content, err := os.ReadFile(filepath.Join("testdata", "agent_profile_skill_routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(content, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("routing corpus has no cases")
	}

	shared := make(map[string]struct{})
	for _, summary := range snapshot.Skills() {
		shared[summary.Name] = struct{}{}
	}
	private := make(map[string]string)
	for _, profile := range catalog.List() {
		for _, skill := range profile.Skills {
			private[skill.Name] = profile.Code
		}
	}
	positiveCoverage := make(map[string]bool)
	excludedCoverage := make(map[string]bool)
	noSkillCase := false
	multiSkillProfiles := make(map[string]bool)
	for _, item := range corpus.Cases {
		t.Run(item.Name, func(t *testing.T) {
			profile, found := catalog.Find(item.Profile)
			if !found || strings.TrimSpace(item.Prompt) == "" {
				t.Fatalf("invalid profile/prompt: %q / %q", item.Profile, item.Prompt)
			}
			available := make(map[string]struct{}, len(shared)+len(profile.Skills))
			for name := range shared {
				available[name] = struct{}{}
			}
			for _, skill := range profile.Skills {
				available[skill.Name] = struct{}{}
			}
			expected := make(map[string]struct{}, len(item.ExpectedSkills))
			for _, name := range item.ExpectedSkills {
				if _, ok := available[name]; !ok {
					t.Errorf("expected Skill %q is unavailable to Profile %q", name, item.Profile)
				}
				expected[name] = struct{}{}
				if owner, ok := private[name]; ok && owner == item.Profile {
					positiveCoverage[name] = true
				}
			}
			for _, name := range item.ExcludedSkills {
				if _, ok := available[name]; !ok {
					t.Errorf("excluded Skill %q is unavailable to Profile %q", name, item.Profile)
				}
				if _, overlaps := expected[name]; overlaps {
					t.Errorf("Skill %q is both expected and excluded", name)
				}
				if owner, ok := private[name]; ok && owner == item.Profile {
					excludedCoverage[name] = true
				}
			}
			if len(item.ExpectedSkills) == 0 {
				noSkillCase = true
			}
			if len(item.ExpectedSkills) > 1 {
				multiSkillProfiles[item.Profile] = true
			}
		})
	}
	for name := range private {
		if !positiveCoverage[name] || !excludedCoverage[name] {
			t.Errorf("private Skill %q coverage: positive=%v excluded=%v", name, positiveCoverage[name], excludedCoverage[name])
		}
	}
	if !noSkillCase {
		t.Error("routing corpus must include a no-Skill case")
	}
	for _, profile := range []string{"health", "legal", "automotive"} {
		if !multiSkillProfiles[profile] {
			t.Errorf("routing corpus must include a multi-Skill %s case", profile)
		}
	}
}

func TestWritingProfileFollowupFormattingContract(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	catalog, err := agentprofiledriver.NewCatalog(pi.WorkDir(workspace))
	if err != nil {
		t.Fatal(err)
	}
	writing, found := catalog.Find("writing")
	if !found {
		t.Fatal("writing Profile is missing")
	}
	socialDescription := ""
	for _, skill := range writing.Skills {
		if skill.Name == "social-content" {
			socialDescription = skill.Description
			break
		}
	}
	if !strings.Contains(socialDescription, "代码块输出") {
		t.Fatalf("social-content description does not route formatting follow-ups: %q", socialDescription)
	}

	files := map[string][]string{
		"AGENTS.md": {
			"当前消息中最新、最具体的格式要求优先",
			"代码块是输出容器",
			"本轮必须给出可直接使用的结果",
			"不向用户说明正在选择、读取或执行哪个 Skill",
		},
		"profiles/writing/skills/social-content/SKILL.md": {
			"缺失但不能虚构的事实",
			"明确占位符",
			"只问一个最关键问题",
			"不得再次要求用户先提供信息",
			"代码块内部保持纯文本",
			"不要在代码块外重复成稿",
		},
	}
	for relativePath, required := range files {
		content, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range required {
			if !strings.Contains(string(content), fragment) {
				t.Errorf("%s missing follow-up formatting contract %q", relativePath, fragment)
			}
		}
	}
}

func TestDefaultChatWorkspaceSkillBodiesAreReadableThroughWebRuntime(t *testing.T) {
	workspace := filepath.Join("..", "..", "workspaces", "chat")
	catalog, err := agentprofiledriver.NewCatalog(pi.WorkDir(workspace))
	if err != nil {
		t.Fatal(err)
	}
	var runtime pi.ToolRuntime
	app := fxtest.New(t,
		agentRegister,
		fx.Supply(
			pi.WorkDir(workspace),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
		fx.Populate(&runtime),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	relativePaths := []string{
		"skills/decision-support/SKILL.md",
		"skills/learning-explanation/SKILL.md",
		"skills/weather-assistance/SKILL.md",
		"skills/writing-assistance/SKILL.md",
	}
	for _, profile := range catalog.List() {
		for _, skill := range profile.Skills {
			relativePaths = append(relativePaths, skill.Location)
		}
	}
	if len(relativePaths) != 25 {
		t.Fatalf("readable Skill paths = %d, want 25", len(relativePaths))
	}
	for _, relativePath := range relativePaths {
		arguments, err := json.Marshal(map[string]any{"path": relativePath})
		if err != nil {
			t.Fatal(err)
		}
		result, err := runtime.Execute(context.Background(), ai.ToolCall{
			ID: "read-skill", Name: "read", Arguments: arguments,
		}, nil)
		if err != nil || result.IsError {
			t.Fatalf("read %s: result = %#v, error = %v", relativePath, result, err)
		}
		got, err := ai.TextContent(result.Content)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Fatalf("read %s returned %q, want full body %q", relativePath, got, want)
		}
	}
}

func TestNewChatWorkDirRejectsInvalidWorkspace(t *testing.T) {
	file := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{name: "nil config", want: "配置"},
		{name: "missing", cfg: &config.Config{Agent: config.AgentConfig{WorkspaceDir: filepath.Join(t.TempDir(), "missing")}}, want: "检查"},
		{name: "regular file", cfg: &config.Config{Agent: config.AgentConfig{WorkspaceDir: file}}, want: "必须是目录"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewChatWorkDir(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "Agent Workspace") {
				t.Fatalf("NewChatWorkDir() error = %v, want Agent Workspace error containing %q", err, tt.want)
			}
		})
	}
}

func TestNewChatWorkDirRejectsProcessWorkingDirectory(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewChatWorkDir(&config.Config{Agent: config.AgentConfig{WorkspaceDir: workingDir}})
	if err == nil || !strings.Contains(err.Error(), "不能使用进程当前目录") {
		t.Fatalf("NewChatWorkDir() error = %v", err)
	}
}
