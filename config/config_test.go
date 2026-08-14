package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigParsesConversationAndMySQLConfiguration(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"deepseek",
		"platforms":[{"id":"deepseek","protocol":"openai","baseURL":"https://example.test/v1/","apiKey":"key","model":"model","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}],
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
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0.15,"output_usd_per_million_tokens":0.60}}]
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
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Host != "127.0.0.1" || cfg.HTTP.Port != "8080" ||
		cfg.HTTP.ReadTimeout != 30 || cfg.HTTP.WriteTimeout != 0 || cfg.HTTP.SecureCookies {
		t.Fatalf("HTTP = %#v", cfg.HTTP)
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
				conversation + `,"mysql":{` + mysql + `}}`

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
		]
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
		]
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
		]
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
		]
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
		]
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
