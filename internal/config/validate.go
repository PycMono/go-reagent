package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func (c *Config) normalizeAndValidate() error {
	c.CurrentPlatform = strings.TrimSpace(c.CurrentPlatform)
	if c.CurrentPlatform == "" {
		return errors.New("currentPlatform 不能为空")
	}
	if len(c.Platforms) == 0 {
		return errors.New("platforms 不能为空")
	}
	if err := c.Bot.normalizeAndValidate(); err != nil {
		return err
	}
	if err := c.Conversation.normalizeAndValidate(&c.MySQL); err != nil {
		return err
	}

	ids := make(map[string]struct{}, len(c.Platforms))
	for index := range c.Platforms {
		platform := &c.Platforms[index]
		platform.normalize()
		if err := platform.validate(index); err != nil {
			return err
		}
		if _, exists := ids[platform.ID]; exists {
			return fmt.Errorf("platforms[%d].id %q 重复", index, platform.ID)
		}
		ids[platform.ID] = struct{}{}
	}

	current, err := c.Current()
	if err != nil {
		return err
	}
	if current.APIKey == "" {
		return fmt.Errorf("当前平台 %q 未配置 apiKey", current.ID)
	}

	return nil
}

func (c *ConversationConfig) normalizeAndValidate(mysql *MySQLConfig) error {
	if c.HistoryMessageLimit == 0 {
		c.HistoryMessageLimit = DefaultHistoryMessageLimit
	}
	if c.HistoryMessageLimit < 1 {
		return errors.New("conversation.history_message_limit 必须大于 0")
	}
	if !c.Enabled {
		return nil
	}
	return mysql.normalizeAndValidate()
}

func (m *MySQLConfig) normalizeAndValidate() error {
	m.Host = strings.TrimSpace(m.Host)
	m.Database = strings.TrimSpace(m.Database)
	m.User = strings.TrimSpace(m.User)
	switch {
	case m.Host == "":
		return errors.New("mysql.host 不能为空")
	case m.Port < 1 || m.Port > 65535:
		return errors.New("mysql.port 必须在 1 到 65535 之间")
	case m.Database == "":
		return errors.New("mysql.database 不能为空")
	case m.User == "":
		return errors.New("mysql.user 不能为空")
	case m.Password == "":
		return errors.New("mysql.password 不能为空")
	case m.MaxOpen < 1:
		return errors.New("mysql.max_open 必须大于 0")
	case m.MaxIdle < 0 || m.MaxIdle > m.MaxOpen:
		return errors.New("mysql.max_idle 必须在 0 到 mysql.max_open 之间")
	case m.ConnLifetime < 1:
		return errors.New("mysql.conn_lifetime 必须大于 0")
	case m.ConnTimeout < 1:
		return errors.New("mysql.conn_timeout 必须大于 0")
	case m.LogLevel < 1 || m.LogLevel > 4:
		return errors.New("mysql.log_level 必须在 1 到 4 之间")
	case m.SlowThreshold < 0:
		return errors.New("mysql.slow_threshold 不能小于 0")
	default:
		return nil
	}
}

func (c *BotConfig) normalizeAndValidate() error {
	c.WeCom.WebhookURL = strings.TrimSpace(c.WeCom.WebhookURL)
	if c.WeCom.WebhookURL == "" {
		return nil
	}
	parsed, err := url.Parse(c.WeCom.WebhookURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("bot.wecom.webhookURL 必须是带 Host 的 HTTPS URL")
	}
	return nil
}

func (p *PlatformConfig) normalize() {
	p.ID = strings.TrimSpace(p.ID)
	p.Protocol = strings.ToLower(strings.TrimSpace(p.Protocol))
	p.BaseURL = strings.TrimSpace(p.BaseURL)
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.Model = strings.TrimSpace(p.Model)
}

func (p *PlatformConfig) validate(index int) error {
	prefix := fmt.Sprintf("platforms[%d]", index)
	if p.ID == "" {
		return fmt.Errorf("%s.id 不能为空", prefix)
	}
	if p.Protocol != ProtocolOpenAI && p.Protocol != ProtocolAnthropic {
		return fmt.Errorf("%s.protocol %q 不受支持，可选值: %s, %s", prefix, p.Protocol, ProtocolOpenAI, ProtocolAnthropic)
	}
	if err := p.normalizeBaseURL(); err != nil {
		return fmt.Errorf("%s.baseURL: %w", prefix, err)
	}
	if p.Model == "" {
		return fmt.Errorf("%s.model 不能为空", prefix)
	}
	return nil
}

func (p *PlatformConfig) normalizeBaseURL() error {
	parsed, err := url.Parse(p.BaseURL)
	if err != nil {
		return fmt.Errorf("不是合法 URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("必须是带 Host 的 HTTP/HTTPS 地址")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("不能包含用户信息、查询参数或片段")
	}
	p.BaseURL = strings.TrimRight(p.BaseURL, "/") + "/"
	return nil
}
