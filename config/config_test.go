package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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
		{name: "negative db", redis: `{"addr":["127.0.0.1:6379"],"password":"` + credential + `","db":-1,"pool_size":5}`, want: "redis.db"},
		{name: "zero pool", redis: `{"addr":["127.0.0.1:6379"],"password":"` + credential + `","db":0,"pool_size":0}`, want: "redis.pool_size"},
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
	tests := []struct {
		name      string
		agent     string
		workspace string
	}{
		{name: "missing", agent: `,"agent":{"limits":{"max_turns":5}}`, workspace: DefaultAgentWorkspaceDir},
		{name: "blank", agent: `,"agent":{"workspace_dir":"  ","limits":{"max_turns":5}}`, workspace: DefaultAgentWorkspaceDir},
		{name: "trimmed", agent: `,"agent":{"workspace_dir":"  ./workspaces/legal  ","limits":{"max_turns":5}}`, workspace: "./workspaces/legal"},
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
				t.Fatalf("WorkspaceDir = %q, want %q", cfg.Agent.WorkspaceDir, tt.workspace)
			}
		})
	}
}

func TestLoadConfigRejectsInvalidHTTPServer(t *testing.T) {
	tests := []struct {
		name string
		http string
		want string
	}{
		{name: "invalid port", http: `{"port":"invalid"}`, want: "http.port"},
		{name: "negative read timeout", http: `{"read_timeout":-1}`, want: "http.read_timeout"},
		{name: "negative write timeout", http: `{"write_timeout":-1}`, want: "http.write_timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, `{
				"currentPlatform":"x",
				"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
				"agent":{"limits":{"max_turns":5}},
				"http":`+tt.http+`
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
		{name: "negative history limit", oldValue: `"history_message_limit":100`, invalidValue: `"history_message_limit":-1`, want: "history_message_limit"},
		{name: "empty host", oldValue: `"host":"127.0.0.1"`, invalidValue: `"host":" "`, want: "mysql.host"},
		{name: "zero port", oldValue: `"port":3306`, invalidValue: `"port":0`, want: "mysql.port"},
		{name: "port above maximum", oldValue: `"port":3306`, invalidValue: `"port":65536`, want: "mysql.port"},
		{name: "empty database", oldValue: `"database":"biz"`, invalidValue: `"database":" "`, want: "mysql.database"},
		{name: "empty user", oldValue: `"user":"root"`, invalidValue: `"user":" "`, want: "mysql.user"},
		{name: "empty password", oldValue: `"password":"` + credential + `"`, invalidValue: `"password":""`, want: "mysql.password"},
		{name: "zero max open", oldValue: `"max_open":100`, invalidValue: `"max_open":0`, want: "mysql.max_open"},
		{name: "negative max idle", oldValue: `"max_idle":10`, invalidValue: `"max_idle":-1`, want: "mysql.max_idle"},
		{name: "max idle above max open", oldValue: `"max_idle":10`, invalidValue: `"max_idle":101`, want: "mysql.max_idle"},
		{name: "zero connection lifetime", oldValue: `"conn_lifetime":3600`, invalidValue: `"conn_lifetime":0`, want: "mysql.conn_lifetime"},
		{name: "zero connection timeout", oldValue: `"conn_timeout":3`, invalidValue: `"conn_timeout":0`, want: "mysql.conn_timeout"},
		{name: "log level below range", oldValue: `"log_level":3`, invalidValue: `"log_level":0`, want: "mysql.log_level"},
		{name: "log level above range", oldValue: `"log_level":3`, invalidValue: `"log_level":5`, want: "mysql.log_level"},
		{name: "negative slow threshold", oldValue: `"slow_threshold":500`, invalidValue: `"slow_threshold":-1`, want: "mysql.slow_threshold"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conversation := `"conversation":{"enabled":true,"history_message_limit":100}`
			mysql := validMySQL
			if strings.Contains(tt.oldValue, "history_message_limit") {
				conversation = strings.Replace(conversation, tt.oldValue, tt.invalidValue, 1)
			} else {
				mysql = strings.Replace(mysql, tt.oldValue, tt.invalidValue, 1)
			}
			document := `{"currentPlatform":"x","platforms":[` +
				`{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}],` +
				`"agent":{"limits":{"max_turns":5}},` + conversation + `,"mysql":{` + mysql + `}}`

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
		"bot":{"wecom":{"webhookURL":" https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-key "}}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Bot.WeCom.WebhookURL; got != "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-key" {
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
	if cfg.Bot.WeCom.WebhookURL != "" {
		t.Fatalf("WebhookURL = %q, want empty", cfg.Bot.WeCom.WebhookURL)
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
		"bot":{"wecom":{"webhookURL":"http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=`+credential+`"}}
	}`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "webhookURL") {
		t.Fatalf("Load() error = %v, want webhookURL validation error", err)
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
	tests := []struct {
		name    string
		servers string
		want    string
	}{
		{name: "duplicate names", servers: `[
			{"name":"exa","enabled":true,"required":true,"url":"https://one.test/mcp","allow_tools":["a"]},
			{"name":"exa","enabled":true,"required":true,"url":"https://two.test/mcp","allow_tools":["b"]}
		]`, want: "name"},
		{name: "blank name", servers: `[{"name":" ","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"]}]`, want: "name"},
		{name: "blank URL", servers: `[{"name":"x","enabled":true,"required":true,"url":" ","allow_tools":["a"]}]`, want: "url"},
		{name: "optional unsupported", servers: `[{"name":"x","enabled":true,"required":false,"url":"https://x.test/mcp","allow_tools":["a"]}]`, want: "required"},
		{name: "negative timeout", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","timeout":-1,"allow_tools":["a"]}]`, want: "timeout"},
		{name: "public HTTP", servers: `[{"name":"x","enabled":true,"required":true,"url":"http://example.com/mcp","allow_tools":["a"]}]`, want: "https"},
		{name: "userinfo", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://user:` + secret + `@x.test/mcp","allow_tools":["a"]}]`, want: "userinfo"},
		{name: "query", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp?key=` + secret + `","allow_tools":["a"]}]`, want: "query"},
		{name: "fragment", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp#fragment","allow_tools":["a"]}]`, want: "fragment"},
		{name: "blank allowlist", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":[]}]`, want: "allow_tools"},
		{name: "duplicate allowlist", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":[" a ","a"]}]`, want: "allow_tools"},
		{name: "invalid prefix", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"tool_prefix":"bad prefix"}]`, want: "tool_prefix"},
		{name: "blank env", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"x-api-key":" "}}]`, want: "header_env"},
		{name: "invalid header name", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"Bad Header":"A"}}]`, want: "header_env"},
		{name: "duplicate header", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"x-api-key":"A","X-Api-Key":"B"}}]`, want: "header_env"},
		{name: "blocked host", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"Host":"A"}}]`, want: "header_env"},
		{name: "blocked length", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"Content-Length":"A"}}]`, want: "header_env"},
		{name: "blocked session", servers: `[{"name":"x","enabled":true,"required":true,"url":"https://x.test/mcp","allow_tools":["a"],"header_env":{"Mcp-Session-Id":"A"}}]`, want: "header_env"},
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
