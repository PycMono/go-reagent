package config

import (
	"errors"
	"path/filepath"
	"strings"
)

type AgentRuntimeConfig struct {
	MaxInstances                         int `json:"max_instances" yaml:"max_instances" toml:"max_instances"`
	MaxInstancesPerTenant                int `json:"max_instances_per_tenant" yaml:"max_instances_per_tenant" toml:"max_instances_per_tenant"`
	ValidationReservedInstances          int `json:"validation_reserved_instances" yaml:"validation_reserved_instances" toml:"validation_reserved_instances"`
	ValidationReservedInstancesPerTenant int `json:"validation_reserved_instances_per_tenant" yaml:"validation_reserved_instances_per_tenant" toml:"validation_reserved_instances_per_tenant"`
	IdleTTLSeconds                       int `json:"idle_ttl_seconds" yaml:"idle_ttl_seconds" toml:"idle_ttl_seconds"`
	AcquireTimeoutSeconds                int `json:"acquire_timeout_seconds" yaml:"acquire_timeout_seconds" toml:"acquire_timeout_seconds"`
}
type ApprovedDependency struct {
	Path   string `json:"path" yaml:"path" toml:"path"`
	SHA256 string `json:"sha256" yaml:"sha256" toml:"sha256"`
}
type ApprovedInterpreter struct {
	Extension    string               `json:"extension" yaml:"extension" toml:"extension"`
	Executable   string               `json:"executable" yaml:"executable" toml:"executable"`
	SHA256       string               `json:"sha256" yaml:"sha256" toml:"sha256"`
	Dependencies []ApprovedDependency `json:"dependencies" yaml:"dependencies" toml:"dependencies"`
}
type AgentTrainingConfig struct {
	SessionTTLSeconds    int                   `json:"session_ttl_seconds" yaml:"session_ttl_seconds" toml:"session_ttl_seconds"`
	ValidationTTLSeconds int                   `json:"validation_ttl_seconds" yaml:"validation_ttl_seconds" toml:"validation_ttl_seconds"`
	SmokeTimeoutSeconds  int                   `json:"smoke_timeout_seconds" yaml:"smoke_timeout_seconds" toml:"smoke_timeout_seconds"`
	ApprovedInterpreters []ApprovedInterpreter `json:"approved_interpreters" yaml:"approved_interpreters" toml:"approved_interpreters"`
}

func (c *Config) normalizeAgentPlatform() error {
	if c.AgentDataDir == "" {
		c.AgentDataDir = "./data/agents"
	}
	r := &c.AgentRuntime
	defaults := []struct {
		dst   *int
		value int
	}{{&r.MaxInstances, 32}, {&r.MaxInstancesPerTenant, 8}, {&r.ValidationReservedInstances, 2}, {&r.ValidationReservedInstancesPerTenant, 1}, {&r.IdleTTLSeconds, 900}, {&r.AcquireTimeoutSeconds, 5}, {&c.AgentTraining.SessionTTLSeconds, 86400}, {&c.AgentTraining.ValidationTTLSeconds, 1800}, {&c.AgentTraining.SmokeTimeoutSeconds, 30}}
	for _, f := range defaults {
		if *f.dst == 0 {
			*f.dst = f.value
		}
		if *f.dst < 0 {
			return errors.New("agent platform limits must be positive")
		}
	}
	if r.MaxInstancesPerTenant > r.MaxInstances || r.ValidationReservedInstances >= r.MaxInstances || r.ValidationReservedInstancesPerTenant >= r.MaxInstancesPerTenant || r.ValidationReservedInstancesPerTenant > r.ValidationReservedInstances {
		return errors.New("invalid agent runtime capacity or reservations")
	}
	seen := map[string]bool{}
	for _, i := range c.AgentTraining.ApprovedInterpreters {
		if !strings.HasPrefix(i.Extension, ".") || strings.ContainsAny(i.Extension, "/\\") || seen[i.Extension] || !filepath.IsAbs(i.Executable) || !lowerSHA256(i.SHA256) {
			return errors.New("invalid approved interpreter pin")
		}
		seen[i.Extension] = true
		for _, d := range i.Dependencies {
			if !filepath.IsAbs(d.Path) || !lowerSHA256(d.SHA256) {
				return errors.New("invalid approved dependency pin")
			}
		}
	}
	return nil
}
func lowerSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
