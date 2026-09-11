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

// devAdminTokenMinLength 是 dev_admin_token 的最低长度要求。该令牌等价于管理
// 员权限，即使仅限本地开发，也不能是弱口令。
const devAdminTokenMinLength = 16

type IdentityConfig struct {
	// Mode 是部署方的明确身份选择。anonymous 固定单租户并签发访客身份；
	// host 要求宿主通过 identity.Authenticator 注入可信身份。
	Mode     string `json:"mode" yaml:"mode" toml:"mode"`
	TenantID string `json:"tenant_id" yaml:"tenant_id" toml:"tenant_id"`
	// DevAdminToken 是本地开发用的管理员令牌：持有令牌（Cookie 或
	// X-Dev-Admin-Token 头）的请求在 anonymous 模式下升级为管理员。默认空，
	// 即不开启任何管理能力；host 模式必须依赖宿主认证，不接受该令牌。
	DevAdminToken string `json:"dev_admin_token" yaml:"dev_admin_token" toml:"dev_admin_token"`
}

// AdminEnabled 表示管理页面与管理 API 是否可用：host 模式由宿主认证提供
// 管理员，anonymous 模式仅在显式配置 dev_admin_token 的本地开发部署开放。
func (config IdentityConfig) AdminEnabled() bool {
	return config.Mode == IdentityModeHost || config.DevAdminToken != ""
}

func (config *IdentityConfig) normalizeAndValidate() error {
	config.Mode = strings.TrimSpace(config.Mode)
	config.TenantID = strings.TrimSpace(config.TenantID)
	config.DevAdminToken = strings.TrimSpace(config.DevAdminToken)
	if config.DevAdminToken != "" && config.Mode != IdentityModeAnonymous {
		return errors.New("identity.dev_admin_token 只适用于 anonymous 本地开发模式；host 模式请提供 identity.Authenticator")
	}
	if len(config.DevAdminToken) > 0 && len(config.DevAdminToken) < devAdminTokenMinLength {
		return errors.New("identity.dev_admin_token 长度不能少于 16 个字符")
	}
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