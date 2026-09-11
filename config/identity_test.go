package config

import (
	"strings"
	"testing"
)

func TestIdentityConfigValidation(t *testing.T) {
	for _, tt := range []struct {
		name, identity string
		wantMode       string
		wantTenant     string
		wantToken      string
		wantError      string
	}{
		{name: "omitted for CLI", identity: "", wantMode: ""},
		{name: "anonymous", identity: `,"identity":{"mode":" anonymous ","tenant_id":" tenant-a "}`, wantMode: "anonymous", wantTenant: "tenant-a"},
		{name: "host", identity: `,"identity":{"mode":"host"}`, wantMode: "host"},
		{name: "unknown", identity: `,"identity":{"mode":"header"}`, wantError: "identity.mode"},
		{name: "anonymous without tenant", identity: `,"identity":{"mode":"anonymous"}`, wantError: "identity.tenant_id"},
		{name: "anonymous invalid tenant", identity: `,"identity":{"mode":"anonymous","tenant_id":"../other"}`, wantError: "identity.tenant_id"},
		{name: "host with tenant", identity: `,"identity":{"mode":"host","tenant_id":"tenant-a"}`, wantError: "identity.tenant_id"},
		{name: "anonymous with dev token", identity: `,"identity":{"mode":"anonymous","tenant_id":"tenant-a","dev_admin_token":"local-dev-admin-0001"}`, wantMode: "anonymous", wantTenant: "tenant-a", wantToken: "local-dev-admin-0001"},
		{name: "anonymous with short dev token", identity: `,"identity":{"mode":"anonymous","tenant_id":"tenant-a","dev_admin_token":"short"}`, wantError: "identity.dev_admin_token"},
		{name: "host with dev token", identity: `,"identity":{"mode":"host","dev_admin_token":"local-dev-admin-0001"}`, wantError: "identity.dev_admin_token"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			document := identityConfigDocument(t, tt.identity)
			cfg, err := Load(writeConfig(t, document), WithAllowProcessCWD())
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("Load() error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Identity.Mode != tt.wantMode || cfg.Identity.TenantID != tt.wantTenant || cfg.Identity.DevAdminToken != tt.wantToken {
				t.Fatalf("identity = %+v", cfg.Identity)
			}
		})
	}
}

func identityConfigDocument(t *testing.T, identity string) string {
	t.Helper()
	return `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{}}],
		"agent":{"workspace_dir":` + quoteJSON(t, t.TempDir()) + `,"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"]}` + identity + `
	}`
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	return `"` + strings.ReplaceAll(value, `\`, `\\`) + `"`
}
