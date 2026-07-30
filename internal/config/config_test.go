package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSelectsAndNormalizesCurrentPlatform(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform": " deepseek ",
		"platforms": [
			{
				"id": " deepseek ",
				"protocol": " OpenAI ",
				"baseURL": "https://api.deepseek.com/v1",
				"apiKey": " deep-key ",
				"model": " deepseek-chat "
			},
			{
				"id": "zhipu-claude",
				"protocol": "anthropic",
				"baseURL": "https://open.bigmodel.cn/api/anthropic/",
				"apiKey": "",
				"model": "glm-4.5-air"
			}
		]
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.Current()
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

func TestLoadNormalizesOptionalWeComWebhookURL(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"deepseek",
		"platforms":[
			{"id":"deepseek","protocol":"openai","baseURL":"https://api.deepseek.com/v1/","apiKey":"key","model":"model"}
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

func TestLoadAllowsMissingWeComWebhookURL(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"deepseek",
		"platforms":[
			{"id":"deepseek","protocol":"openai","baseURL":"https://api.deepseek.com/v1/","apiKey":"key","model":"model"}
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

func TestLoadRejectsUnsafeWeComWebhookURLWithoutLeakingIt(t *testing.T) {
	const credential = "never-print-webhook-key"
	path := writeConfig(t, `{
		"currentPlatform":"deepseek",
		"platforms":[
			{"id":"deepseek","protocol":"openai","baseURL":"https://api.deepseek.com/v1/","apiKey":"key","model":"model"}
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

func TestLoadSupportsYAMLAndTOML(t *testing.T) {
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
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(writeConfigFile(t, "config"+tt.extension, tt.document))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			current, err := cfg.Current()
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

func TestLoadAppliesShellEnvironmentOverride(t *testing.T) {
	t.Setenv("CONFIGOR_CURRENTPLATFORM", "backup")
	path := writeConfig(t, `{
		"currentPlatform":"primary",
		"platforms":[
			{"id":"primary","protocol":"openai","baseURL":"https://primary.test/","apiKey":"primary-key","model":"primary-model"},
			{"id":"backup","protocol":"anthropic","baseURL":"https://backup.test/","apiKey":"backup-key","model":"backup-model"}
		]
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.Current()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.ID != "backup" || current.Model != "backup-model" {
		t.Fatalf("current = %#v, want backup platform", current)
	}
}

func TestLoadAppliesEnvironmentFileOverlay(t *testing.T) {
	t.Setenv("CONFIGOR_ENV", "test")
	dir := t.TempDir()
	path := writeConfigAt(t, dir, "config.json", `{
		"currentPlatform":"primary",
		"platforms":[
			{"id":"primary","protocol":"openai","baseURL":"https://primary.test/","apiKey":"primary-key","model":"primary-model"},
			{"id":"backup","protocol":"anthropic","baseURL":"https://backup.test/","apiKey":"backup-key","model":"backup-model"}
		]
	}`)
	writeConfigAt(t, dir, "config.test.json", `{"currentPlatform":"backup"}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.Current()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.ID != "backup" || current.Model != "backup-model" {
		t.Fatalf("current = %#v, want environment overlay to select backup", current)
	}
}

func TestLoadFallsBackToExampleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigAt(t, dir, "config.example.json", `{
		"currentPlatform":"example",
		"platforms":[
			{"id":"example","protocol":"openai","baseURL":"https://example.test/","apiKey":"example-key","model":"example-model"}
		]
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.Current()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.ID != "example" || current.Model != "example-model" {
		t.Fatalf("current = %#v, want example platform", current)
	}
}

func TestLoadUsesConfigorPermissiveJSONDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[
			{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m"}
		],
		"unknown":true
	} {"ignored":"trailing document"}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	current, err := cfg.Current()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if current.ID != "x" {
		t.Fatalf("current ID = %q, want x", current.ID)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     string
	}{
		{
			name: "empty current platform",
			document: `{"currentPlatform":" ","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m"}
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
				{"id":" ","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m"}
			]}`,
			want: "id",
		},
		{
			name: "duplicate id",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m","apiKey":"k"},
				{"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m"}
			]}`,
			want: "重复",
		},
		{
			name: "missing current",
			document: `{"currentPlatform":"missing","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m"}
			]}`,
			want: "可用平台",
		},
		{
			name: "missing current key",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/v1/","model":"m"}
			]}`,
			want: "apiKey",
		},
		{
			name: "unsupported protocol",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"other","baseURL":"https://x.test/","apiKey":"never-print-this","model":"m"}
			]}`,
			want: "protocol",
		},
		{
			name: "invalid URL scheme",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"file:///tmp/x","apiKey":"k","model":"m"}
			]}`,
			want: "baseURL",
		},
		{
			name: "URL without host",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https:///v1","apiKey":"k","model":"m"}
			]}`,
			want: "baseURL",
		},
		{
			name: "empty model",
			document: `{"currentPlatform":"x","platforms":[
				{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":" "}
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

func TestLoadErrorContainsConfigurationPath(t *testing.T) {
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
