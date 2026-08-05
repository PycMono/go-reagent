package config

import (
	"errors"
	"net/url"
	"strings"
)

func (config *Config) normalizeAndValidate() error {
	if err := config.Bot.normalizeAndValidate(); err != nil {
		return err
	}
	return config.Conversation.normalizeAndValidate(&config.MySQL)
}

func (config *ConversationConfig) normalizeAndValidate(mysql *MySQLConfig) error {
	if config.HistoryMessageLimit == 0 {
		config.HistoryMessageLimit = DefaultHistoryMessageLimit
	}
	if config.HistoryMessageLimit < 1 {
		return errors.New("conversation.history_message_limit 必须大于 0")
	}
	if !config.Enabled {
		return nil
	}
	return mysql.normalizeAndValidate()
}

func (config *MySQLConfig) normalizeAndValidate() error {
	config.Host = strings.TrimSpace(config.Host)
	config.Database = strings.TrimSpace(config.Database)
	config.User = strings.TrimSpace(config.User)
	switch {
	case config.Host == "":
		return errors.New("mysql.host 不能为空")
	case config.Port < 1 || config.Port > 65535:
		return errors.New("mysql.port 必须在 1 到 65535 之间")
	case config.Database == "":
		return errors.New("mysql.database 不能为空")
	case config.User == "":
		return errors.New("mysql.user 不能为空")
	case config.Password == "":
		return errors.New("mysql.password 不能为空")
	case config.MaxOpen < 1:
		return errors.New("mysql.max_open 必须大于 0")
	case config.MaxIdle < 0 || config.MaxIdle > config.MaxOpen:
		return errors.New("mysql.max_idle 必须在 0 到 mysql.max_open 之间")
	case config.ConnLifetime < 1:
		return errors.New("mysql.conn_lifetime 必须大于 0")
	case config.ConnTimeout < 1:
		return errors.New("mysql.conn_timeout 必须大于 0")
	case config.LogLevel < 1 || config.LogLevel > 4:
		return errors.New("mysql.log_level 必须在 1 到 4 之间")
	case config.SlowThreshold < 0:
		return errors.New("mysql.slow_threshold 不能小于 0")
	default:
		return nil
	}
}

func (config *BotConfig) normalizeAndValidate() error {
	config.WeCom.WebhookURL = strings.TrimSpace(config.WeCom.WebhookURL)
	if config.WeCom.WebhookURL == "" {
		return nil
	}
	parsed, err := url.Parse(config.WeCom.WebhookURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("bot.wecom.webhookURL 必须是带 Host 的 HTTPS URL")
	}
	return nil
}
