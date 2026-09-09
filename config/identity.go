package config

import (
	"errors"
	"strings"

	"github.com/PycMono/go-reagent/application/identity"
)

const (
	IdentityModeAnonymous = "anonymous"
	IdentityModeHost      = "host"
)

type IdentityConfig struct {
	// Mode 是部署方的明确身份选择。anonymous 固定单租户并签发访客身份；
	// host 要求宿主通过 identity.Authenticator 注入可信身份。
	Mode     string `json:"mode" yaml:"mode" toml:"mode"`
	TenantID string `json:"tenant_id" yaml:"tenant_id" toml:"tenant_id"`
}

func (config *IdentityConfig) normalizeAndValidate() error {
	config.Mode = strings.TrimSpace(config.Mode)
	config.TenantID = strings.TrimSpace(config.TenantID)
	switch config.Mode {
	case "":
		if config.TenantID != "" {
			return errors.New("identity.mode 不能为空")
		}
		return nil
	case IdentityModeAnonymous:
		if !identity.ValidID(config.TenantID) {
			return errors.New("identity.tenant_id 必须是明确的合法租户 ID")
		}
		return nil
	case IdentityModeHost:
		if config.TenantID != "" {
			return errors.New("identity.tenant_id 仅适用于 anonymous 模式")
		}
		return nil
	default:
		return errors.New("identity.mode 只支持 anonymous 或 host")
	}
}
