package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestMain 把包级测试的工作目录切到临时目录并准备默认 Workspace，使
// Load 校验 agent.workspace_dir 时默认值 ./workspaces/chat 真实存在。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "go-reagent-config-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	for _, workspace := range []string{"workspaces/chat", "workspaces/legal"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(workspace)), 0o755); err != nil {
			panic(err)
		}
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestLoadConfigParsesAndNormalizesRequiredRedis(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":[" 127.0.0.1:6379 "],"password":"redis-secret","db":2,"pool_size":5}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Redis.Addr, []string{"127.0.0.1:6379"}) || cfg.Redis.Password != "redis-secret" ||
		cfg.Redis.DB != 2 || cfg.Redis.PoolSize != 5 {
		t.Fatalf("Redis = %#v", cfg.Redis)
	}
}

func TestLoadConfigRejectsInvalidRequiredRedisWithoutLeakingPassword(t *testing.T) {
	const credential = "never-print-redis-password"
	tests := []struct {
		name  string
		redis string
		want  string
	}{
		{name: "missing", redis: `{}`, want: "redis.addr"},
		{name: "empty address", redis: `{"addr":["  "],"password":"` + credential + `","db":0,"pool_size":5}`, want: "redis.addr"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := `{"currentPlatform":"x","platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],"agent":{"limits":{"max_turns":5}},"redis":` + test.redis + `}`
			_, err := Load(writeConfig(t, document))
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), credential) {
				t.Fatalf("Load() error = %v, want %q without credential", err, test.want)
			}
		})
	}
}

func TestLoadConfigParsesConversationAndMySQLConfiguration(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"deepseek",
		"platforms":[{"id":"deepseek","protocol":"openai","baseURL":"https://example.test/v1/","apiKey":"key","model":"model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5},
		"conversation":{"enabled":true,"history_message_limit":100},
		"mysql":{
			"host":"127.0.0.1","port":3306,"database":"biz","user":"root","password":"123456",
			"max_open":100,"max_idle":10,"conn_lifetime":3600,"conn_timeout":3,
			"log_level":3,"slow_threshold":500
		}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Conversation.Enabled || cfg.Conversation.HistoryMessageLimit != 100 {
		t.Fatalf("Conversation = %#v", cfg.Conversation)
	}
	if cfg.MySQL.Host != "127.0.0.1" || cfg.MySQL.Port != 3306 || cfg.MySQL.Database != "biz" ||
		cfg.MySQL.User != "root" || cfg.MySQL.Password != "123456" || cfg.MySQL.MaxOpen != 100 ||
		cfg.MySQL.MaxIdle != 10 || cfg.MySQL.ConnLifetime != 3600 || cfg.MySQL.ConnTimeout != 3 ||
		cfg.MySQL.LogLevel != 3 || cfg.MySQL.SlowThreshold != 500 {
		t.Fatalf("MySQL = %#v", cfg.MySQL)
	}
}

func TestLoadConfigDefaultsConversationHistoryLimit(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Conversation.HistoryMessageLimit != DefaultHistoryMessageLimit {
		t.Fatalf("HistoryMessageLimit = %d", cfg.Conversation.HistoryMessageLimit)
	}
}

func TestLoadConfigDefaultsHTTPServer(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Host != "127.0.0.1" || cfg.HTTP.Port != "8080" ||
		cfg.HTTP.ReadTimeout != 30 || cfg.HTTP.WriteTimeout != 0 || cfg.HTTP.SecureCookies {
		t.Fatalf("HTTP = %#v", cfg.HTTP)
	}
}

func TestLoadConfigDefaultsAndNormalizesAgentWorkspace(t *testing.T) {
	resolvedDefault := mustResolveDirectory(t, DefaultAgentWorkspaceDir)
	resolvedLegal := mustResolveDirectory(t, "./workspaces/legal")
	tests := []struct {
		name      string
		agent     string
		workspace string
	}{
		{name: "missing", agent: `,"agent":{"limits":{"max_turns":5}}`, workspace: resolvedDefault},
		{name: "blank", agent: `,"agent":{"workspace_dir":"  ","limits":{"max_turns":5}}`, workspace: resolvedDefault},
		{name: "trimmed", agent: `,"agent":{"workspace_dir":"  ./workspaces/legal  ","limits":{"max_turns":5}}`, workspace: resolvedLegal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, `{
					"currentPlatform":"x",
					"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
					"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}`+tt.agent+`
				}`))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Agent.WorkspaceDir != tt.workspace {
				t.Fatalf("WorkspaceDir = %q, want resolved absolute path %q", cfg.Agent.WorkspaceDir, tt.workspace)
			}
		})
	}
}

// mustResolveDirectory 用与 Load 相同的解析逻辑计算期望的绝对路径。
func mustResolveDirectory(t *testing.T, path string) string {
	t.Helper()
	resolved, err := resolveDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestLoadConfigRejectsInvalidAgentWorkspaceDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		workspace string
		want      string
	}{
		{name: "missing", workspace: filepath.Join(t.TempDir(), "missing"), want: "agent.workspace_dir"},
		{name: "regular file", workspace: file, want: "必须是目录"},
		{name: "process working directory", workspace: ".", want: "不能使用进程当前目录"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, `{
				"currentPlatform":"x",
				"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
				"agent":{"workspace_dir":"`+tt.workspace+`","limits":{"max_turns":5}},
				"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
			}`))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadConfigRejectsInvalidConversationAndMySQLConfiguration(t *testing.T) {
	const (
		credential = "never-print-mysql-password"
		validMySQL = `"host":"127.0.0.1","port":3306,"database":"biz","user":"root","password":"` + credential + `",` +
			`"max_open":100,"max_idle":10,"conn_lifetime":3600,"conn_timeout":3,"log_level":3,"slow_threshold":500`
	)
	tests := []struct {
		name         string
		oldValue     string
		invalidValue string
		want         string
	}{
		{name: "empty host", oldValue: `"host":"127.0.0.1"`, invalidValue: `"host":" "`, want: "mysql.host"},
		{name: "zero port", oldValue: `"port":3306`, invalidValue: `"port":0`, want: "mysql.port"},
		{name: "port above maximum", oldValue: `"port":3306`, invalidValue: `"port":65536`, want: "mysql.port"},
		{name: "empty database", oldValue: `"database":"biz"`, invalidValue: `"database":" "`, want: "mysql.database"},
		{name: "empty user", oldValue: `"user":"root"`, invalidValue: `"user":" "`, want: "mysql.user"},
		{name: "empty password", oldValue: `"password":"` + credential + `"`, invalidValue: `"password":""`, want: "mysql.password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mysql := strings.Replace(validMySQL, tt.oldValue, tt.invalidValue, 1)
			document := `{"currentPlatform":"x","platforms":[` +
				`{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}],` +
				`"agent":{"limits":{"max_turns":5}},` +
				`"conversation":{"enabled":true,"history_message_limit":100},"mysql":{` + mysql + `}}`

			_, err := Load(writeConfig(t, document))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want containing %q", err, tt.want)
			}
			if strings.Contains(errorText(err), credential) {
				t.Fatalf("Load() error leaks MySQL password: %v", err)
			}
		})
	}
}

func TestLoadConfigSelectsAndNormalizesCurrentPlatform(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform": " deepseek ",
		"platforms": [
			{
				"id": " deepseek ",
				"protocol": " OpenAI ",
				"baseURL": "https://api.deepseek.com/v1",
				"apiKey": " deep-key ",
				"model": " deepseek-chat ","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}
			},
			{
				"id": "zhipu-claude",
				"protocol": "anthropic",
				"baseURL": "https://open.bigmodel.cn/api/anthropic/",
				"apiKey": "",
				"model": "glm-4.5-air","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}
			}
		],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.CurrentPlatformOptions()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}

	if current.ID != "deepseek" || current.Protocol != "openai" {
		t.Fatalf("current identity = %#v", current)
	}
	if current.BaseURL != "https://api.deepseek.com/v1/" {
		t.Fatalf("BaseURL = %q", current.BaseURL)
	}
	if current.APIKey != "deep-key" || current.Model != "deepseek-chat" {
		t.Fatalf("current credentials/model = %#v", current)
	}
	if cfg.Platforms[1].APIKey != "" {
		t.Fatalf("inactive platform APIKey = %q", cfg.Platforms[1].APIKey)
	}
}

func TestLoadConfigNormalizesOptionalWeComWebhookURL(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"deepseek",
		"platforms":[
			{"id":"deepseek","protocol":"openai","baseURL":"https://api.deepseek.com/v1/","apiKey":"key","model":"model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
		],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5},
		"notice":{"wecom":{"webhook_url":" https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-key "}}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Notice.WeCom.WebhookURL; got != "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-key" {
		t.Fatalf("WebhookURL = %q", got)
	}
}

func TestLoadConfigAllowsMissingWeComWebhookURL(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"deepseek",
		"platforms":[
			{"id":"deepseek","protocol":"openai","baseURL":"https://api.deepseek.com/v1/","apiKey":"key","model":"model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
		],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Notice.WeCom.WebhookURL != "" {
		t.Fatalf("WebhookURL = %q, want empty", cfg.Notice.WeCom.WebhookURL)
	}
}

func TestLoadConfigRejectsUnsafeWeComWebhookURLWithoutLeakingIt(t *testing.T) {
	const credential = "never-print-webhook-key"
	path := writeConfig(t, `{
		"currentPlatform":"deepseek",
		"platforms":[
			{"id":"deepseek","protocol":"openai","baseURL":"https://api.deepseek.com/v1/","apiKey":"key","model":"model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
		],
		"agent":{"limits":{"max_turns":5}},
		"notice":{"wecom":{"webhook_url":"http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=`+credential+`"}}
	}`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "webhook_url") {
		t.Fatalf("Load() error = %v, want webhook_url validation error", err)
	}
	if strings.Contains(errorText(err), credential) {
		t.Fatalf("Load() error leaks webhook credential: %v", err)
	}
}

func TestLoadConfigSupportsYAMLAndTOML(t *testing.T) {
	tests := []struct {
		name      string
		extension string
		document  string
	}{
		{
			name:      "YAML",
			extension: ".yaml",
			document: `currentPlatform: " deepseek "
platforms:
  - id: " deepseek "
    protocol: " OpenAI "
    baseURL: "https://api.deepseek.com/v1"
    apiKey: " deep-key "
    model: " deepseek-chat "
    pricing:
      input_usd_per_million_tokens: 0.15
      output_usd_per_million_tokens: 0.60
agent:
  limits:
    max_turns: 5
redis:
  addr:
    - "127.0.0.1:6379"
  password: ""
  db: 0
  pool_size: 5
`,
		},
		{
			name:      "TOML",
			extension: ".toml",
			document: `currentPlatform = " deepseek "

[[platforms]]
id = " deepseek "
protocol = " OpenAI "
baseURL = "https://api.deepseek.com/v1"
apiKey = " deep-key "
model = " deepseek-chat "
[platforms.pricing]
input_usd_per_million_tokens = 0.15
output_usd_per_million_tokens = 0.60

[agent.limits]
max_turns = 5

[redis]
addr = ["127.0.0.1:6379"]
password = ""
db = 0
pool_size = 5
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(writeConfigFile(t, "config"+tt.extension, tt.document))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			current, err := cfg.CurrentPlatformOptions()
			if err != nil {
				t.Fatalf("Current() error = %v", err)
			}

			if current.ID != "deepseek" || current.Protocol != "openai" {
				t.Fatalf("current identity = %#v", current)
			}
			if current.BaseURL != "https://api.deepseek.com/v1/" {
				t.Fatalf("BaseURL = %q", current.BaseURL)
			}
			if current.APIKey != "deep-key" || current.Model != "deepseek-chat" {
				t.Fatalf("current credentials/model = %#v", current)
			}
		})
	}
}

func TestLoadConfigAppliesShellEnvironmentOverride(t *testing.T) {
	t.Setenv("CONFIGOR_CURRENTPLATFORM", "backup")
	path := writeConfig(t, `{
		"currentPlatform":"primary",
		"platforms":[
			{"id":"primary","protocol":"openai","baseURL":"https://primary.test/","apiKey":"primary-key","model":"primary-model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}},
			{"id":"backup","protocol":"anthropic","baseURL":"https://backup.test/","apiKey":"backup-key","model":"backup-model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
		],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.CurrentPlatformOptions()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.ID != "backup" || current.Model != "backup-model" {
		t.Fatalf("current = %#v, want backup platform", current)
	}
}

func TestLoadConfigAppliesEnvironmentFileOverlay(t *testing.T) {
	t.Setenv("CONFIGOR_ENV", "test")
	dir := t.TempDir()
	path := writeConfigAt(t, dir, "config.json", `{
		"currentPlatform":"primary",
		"platforms":[
			{"id":"primary","protocol":"openai","baseURL":"https://primary.test/","apiKey":"primary-key","model":"primary-model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}},
			{"id":"backup","protocol":"anthropic","baseURL":"https://backup.test/","apiKey":"backup-key","model":"backup-model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
		],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
	}`)
	writeConfigAt(t, dir, "config.test.json", `{"currentPlatform":"backup"}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.CurrentPlatformOptions()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.ID != "backup" || current.Model != "backup-model" {
		t.Fatalf("current = %#v, want environment overlay to select backup", current)
	}
}

func TestLoadConfigFallsBackToExampleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigAt(t, dir, "config.example.json", `{
		"currentPlatform":"example",
		"platforms":[
			{"id":"example","protocol":"openai","baseURL":"https://example.test/","apiKey":"example-key","model":"example-model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
		],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.CurrentPlatformOptions()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.ID != "example" || current.Model != "example-model" {
		t.Fatalf("current = %#v, want example platform", current)
	}
}

func TestLoadConfigUsesConfigorPermissiveJSONDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[
			{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
		],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5},
		"unknown":true
	} {"ignored":"trailing document"}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.CurrentPlatformOptions()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.ID != "x" {
		t.Fatalf("current ID = %q, want x", current.ID)
	}
}

func TestLoadConfigRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     string
	}{
		{
			name: "empty current platform",
			document: `{"currentPlatform":" ","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
			]}`,
			want: "currentPlatform",
		},
		{
			name:     "empty platforms",
			document: `{"currentPlatform":"x","platforms":[]}`,
			want:     "platforms",
		},
		{
			name: "empty id",
			document: `{"currentPlatform":"x","platforms":[
				{"id":" ","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
			]}`,
			want: "id",
		},
		{
			name: "duplicate id",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m","apiKey":"k","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}},
				{"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
			]}`,
			want: "重复",
		},
		{
			name: "missing current",
			document: `{"currentPlatform":"missing","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
			]}`,
			want: "可用平台",
		},
		{
			name: "missing current key",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
			]}`,
			want: "apiKey",
		},
		{
			name: "unsupported protocol",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"other","baseURL":"https://x.test/","apiKey":"never-print-this","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
			]}`,
			want: "protocol",
		},
		{
			name: "invalid URL scheme",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"file:///tmp/x","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
			]}`,
			want: "baseURL",
		},
		{
			name: "URL without host",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https:///v1","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
			]}`,
			want: "baseURL",
		},
		{
			name: "empty model",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":" ","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}
			]}`,
			want: "model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.document))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want containing %q", err, tt.want)
			}
			if strings.Contains(errorText(err), "never-print-this") {
				t.Fatalf("Load() error leaks credential: %v", err)
			}
		})
	}
}

func TestLoadConfigErrorContainsConfigurationPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("Load() error = %v, want path %q", err, path)
	}
}

func TestLoadConfigNormalizesMCPServers(t *testing.T) {
	t.Setenv("EXA_API_KEY", "test-key")
	document := validMCPBaseConfig(`"mcp":{"servers":[{
		"name":" exa ","enabled":true,"required":true,
		"url":" https://mcp.exa.ai/mcp ","timeout":0,
		"header_env":{"x-api-key":" EXA_API_KEY "},
		"allow_tools":[" web_search_exa ","web_fetch_exa"],
		"tool_prefix":""
	}]}`)
	cfg, err := Load(writeConfig(t, document))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCP.Servers) != 1 {
		t.Fatalf("MCP servers = %#v", cfg.MCP.Servers)
	}
	server := cfg.MCP.Servers[0]
	if server.Name != "exa" || server.URL != "https://mcp.exa.ai/mcp" || server.Timeout != 60 ||
		server.HeaderEnv["X-Api-Key"] != "EXA_API_KEY" || server.ToolPrefix != "" ||
		!slices.Equal(server.AllowTools, []string{"web_search_exa", "web_fetch_exa"}) {
		t.Fatalf("MCP server = %#v", server)
	}
}

func TestLoadConfigRejectsInvalidMCPServersWithoutLeakingSecrets(t *testing.T) {
	const secret = "never-print-mcp-config-secret"
	t.Setenv("GO_REAGENT_TEST_EMPTY_ENV", "")
	tests := []struct {
		name    string
		servers string
		want    string
	}{
		{name: "blank name", servers: `[{"name":" ","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"]}]`, want: "name"},
		{name: "blank URL", servers: `[{"name":"x","enabled":true,"required":true,"url":" ","allow_tools":["a"]}]`, want: "url"},
		{name: "optional unsupported", servers: `[{"name":"x","enabled":true,"required":false,"url":"https://x.test/mcp","allow_tools":["a"]}]`, want: "required"},
		{name: "public HTTP", servers: `[{"name":"x","enabled":true,"required":true,"url":"http://example.com/mcp","allow_tools":["a"]}]`, want: "https"},
		{name: "blank allowlist", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":[]}]`, want: "allow_tools"},
		{name: "blank env", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"x-api-key":" "}}]`, want: "header_env"},
		{name: "blocked host", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"Host":"A"}}]`, want: "header_env"},
		{name: "blocked length", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"Content-Length":"A"}}]`, want: "header_env"},
		{name: "blocked session", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"Mcp-Session-Id":"A"}}]`, want: "header_env"},
		{name: "unset env", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"x-api-key":"GO_REAGENT_TEST_UNSET_ENV"}}]`, want: "环境变量"},
		{name: "empty env", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"x-api-key":"GO_REAGENT_TEST_EMPTY_ENV"}}]`, want: "环境变量"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, validMCPBaseConfig(`"mcp":{"servers":`+test.servers+`}`)))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) || strings.Contains(err.Error(), secret) {
				t.Fatalf("Load error = %v, want %q without secret", err, test.want)
			}
		})
	}
}

func TestLoadConfigAllowsAbsentAndDisabledMCP(t *testing.T) {
	for _, extra := range []string{
		``,
		`"mcp":{"servers":[{"enabled":false,"name":" ","url":"not a URL"}]}`,
	} {
		cfg, err := Load(writeConfig(t, validMCPBaseConfig(extra)))
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.MCP.Servers) > 0 && cfg.MCP.Servers[0].Enabled {
			t.Fatalf("MCP config = %#v", cfg.MCP)
		}
	}
}

func TestLoadConfigValidatesAgentLimits(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		want  string
	}{
		{name: "all zero", agent: `"agent":{"limits":{}}`, want: "agent.limits"},
		{name: "missing", agent: ``, want: "agent.limits"},
		{name: "negative turns", agent: `"agent":{"limits":{"max_turns":-1}}`, want: "max_turns"},
		{name: "negative tokens", agent: `"agent":{"limits":{"max_total_tokens":-1}}`, want: "max_total_tokens"},
		{name: "negative cost", agent: `"agent":{"limits":{"max_cost_usd":-0.5}}`, want: "max_cost_usd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := tt.agent
			if agent != "" {
				agent += ","
			}
			document := `{"currentPlatform":"x","platforms":[` +
				`{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],` +
				agent + `"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}}`
			_, err := Load(writeConfig(t, document))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadConfigParsesAgentLimits(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
		"agent":{"limits":{"max_turns":20,"max_cost_usd":1.5,"max_total_tokens":2000000}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.Limits.MaxTurns != 20 || cfg.Agent.Limits.MaxCostUSD != 1.5 ||
		cfg.Agent.Limits.MaxTotalTokens != 2_000_000 {
		t.Fatalf("Limits = %#v", cfg.Agent.Limits)
	}
}

func validMCPBaseConfig(extra string) string {
	separator := ""
	if extra != "" {
		separator = ","
	}
	return `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}` + separator + extra + `
	}`
}

func writeConfig(t *testing.T, document string) string {
	t.Helper()
	return writeConfigFile(t, "config.json", document)
}

func writeConfigFile(t *testing.T, name, document string) string {
	t.Helper()
	return writeConfigAt(t, t.TempDir(), name, document)
}

func writeConfigAt(t *testing.T, dir, name, document string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestLoadConfigParsesContextWindowTokens(t *testing.T) {
	document := func(extra string) string {
		return `{
			"currentPlatform":"deepseek",
			"platforms":[{
				"id":"deepseek","protocol":"openai","baseURL":"https://api.deepseek.com/v1/",
				"apiKey":"k","model":"deepseek-chat",` + extra + `
				"pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}
			}],
			"agent":{"limits":{"max_turns":5}},
			"redis":{"addr":["127.0.0.1:6379"],"password":"","db":0,"pool_size":5}
		}`
	}

	t.Run("省略时窗口容量为零", func(t *testing.T) {
		cfg, err := Load(writeConfig(t, document("")))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Platforms[0].ContextWindowTokens != 0 {
			t.Fatalf("ContextWindowTokens = %d, want 0 when omitted", cfg.Platforms[0].ContextWindowTokens)
		}
	})
	t.Run("正值生效", func(t *testing.T) {
		cfg, err := Load(writeConfig(t, document(`"contextWindowTokens":131072,`)))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Platforms[0].ContextWindowTokens != 131072 {
			t.Fatalf("ContextWindowTokens = %d, want 131072", cfg.Platforms[0].ContextWindowTokens)
		}
	})
	t.Run("负值被拒绝", func(t *testing.T) {
		_, err := Load(writeConfig(t, document(`"contextWindowTokens":-1,`)))
		if err == nil {
			t.Fatal("Load() error = nil, want negative contextWindowTokens rejected")
		}
	})
}
