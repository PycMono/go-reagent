package config

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/governor"
	pimcp "github.com/PycMono/go-reagent/pi/mcp"
	"github.com/PycMono/go-reagent/pi/middleware"
)

// CaptureAgentSnapshot resolves defaults once, when creating a new version.
// Historical versions reference platform entries for credentials only. Check
// agent_versions before removing a platform needed for activation or rollback.
func (c *Config) CaptureAgentSnapshot(providerID, modelID string, toolNames []string) (agentversion.Snapshot, error) {
	if providerID == "" {
		providerID = c.CurrentPlatform
	}
	var selected *providers.Options
	for i := range c.Platforms {
		if c.Platforms[i].ID == providerID {
			selected = &c.Platforms[i]
			break
		}
	}
	if selected == nil {
		return agentversion.Snapshot{}, errors.New("model reference unavailable")
	}
	p := *selected
	if modelID != "" && modelID != p.Model {
		return agentversion.Snapshot{}, errors.New("model is not in configured allowed options")
	}
	if err := p.NormalizeAndValidate(); err != nil {
		return agentversion.Snapshot{}, errors.New("model reference invalid or missing credential")
	}
	price := *p.Pricing
	s := agentversion.Snapshot{SchemaVersion: 1, Model: agentversion.ModelConfig{ProviderRef: p.ID, SecretRef: "platform:" + p.ID, Protocol: string(p.Protocol), BaseURL: p.BaseURL, ModelID: p.Model, Pricing: &price, ContextWindowTokens: p.ContextWindowTokens, Vision: p.Vision, Capabilities: []string{"text", "tools"}}}
	s.Tools.Builtin = agentversion.BuiltinTools{Read: true, Exec: true, Subagent: true}
	s.Tools.Registered = []agentversion.ToolRef{}
	s.Tools.MCP = []agentversion.MCPRef{}
	s.Tools.Permissions = []agentversion.PermissionRule{}
	for _, name := range toolNames {
		s.Tools.Registered = append(s.Tools.Registered, agentversion.ToolRef{Name: name, ImplementationRef: "application:" + name + ":v1", Arguments: map[string]string{}})
	}
	for _, server := range c.MCP.Servers {
		if server.Enabled {
			s.Tools.MCP = append(s.Tools.MCP, agentversion.MCPRef{Name: server.Name, ImplementationRef: "mcp:v1", ConfigRef: MCPConfigReference(server), AllowTools: append([]string{}, server.AllowTools...), ToolPrefix: server.ToolPrefix})
		}
	}
	for _, rule := range c.Permissions.Rules {
		s.Tools.Permissions = append(s.Tools.Permissions, agentversion.PermissionRule{Tool: rule.Tool, Effect: rule.Effect, Reason: rule.Reason, Patterns: append([]string{}, rule.Patterns...)})
	}
	limits := c.Agent.Limits
	defaults := governor.DefaultLimits()
	if limits.MaxTurns == 0 {
		limits.MaxTurns = defaults.MaxTurns
	}
	if limits.MaxCostUSD == 0 {
		limits.MaxCostUSD = defaults.MaxCostUSD
	}
	if limits.MaxTotalTokens == 0 {
		limits.MaxTotalTokens = defaults.MaxTotalTokens
	}
	policy := agentversion.WorkspacePolicy{WriteMode: "restricted", WritablePrefixes: []string{".tmp", "scratch"}}
	if c.Agent.WorkspacePolicy != nil {
		if c.Agent.WorkspacePolicy.WriteMode != "restricted" {
			return s, errors.New("production version requires restricted workspace")
		}
		policy.WritablePrefixes = append([]string{}, c.Agent.WorkspacePolicy.WritablePrefixes...)
	}
	retry := c.Tools.Retry
	retry.Attempts = max(1, min(retry.Attempts, middleware.MaxRetryAttempts))
	if retry.BackoffMs <= 0 {
		retry.BackoffMs = int(middleware.DefaultRetryBackoff / time.Millisecond)
	}
	history := c.Conversation.HistoryMessageLimit
	if history <= 0 {
		history = DefaultHistoryMessageLimit
	}
	s.Runtime = agentversion.RuntimeConfig{Limits: limits, LoopDetection: c.Agent.LoopDetection, HistoryMessageLimit: history, WritePolicy: policy, AllowExec: true, AllowWrite: false,
		Compaction:  agentversion.CompactionConfig{ContextWindowTokens: p.ContextWindowTokens, EnablePrune: c.Agent.EnableContextPrune},
		ToolRuntime: agentversion.ToolRuntimeConfig{TimeoutSeconds: max(0, c.Tools.TimeoutSeconds), Retry: agentversion.ToolRetryConfig{Attempts: retry.Attempts, BackoffMS: retry.BackoffMs, Tools: append([]string{}, retry.Tools...)}}}
	s.Runtime.LoopDetection.ExcludedTools = append([]string{}, s.Runtime.LoopDetection.ExcludedTools...)
	encoded, err := json.Marshal(s)
	if err != nil {
		return s, err
	}
	return agentversion.ParseSnapshot(encoded)
}

func (c *Config) ResolveAgentModel(saved agentversion.ModelConfig) (providers.Options, error) {
	if saved.SecretRef != "platform:"+saved.ProviderRef || saved.Reasoning != "" || saved.Pricing == nil {
		return providers.Options{}, errors.New("unsupported model snapshot")
	}
	for _, configured := range c.Platforms {
		if configured.ID == saved.ProviderRef && configured.APIKey != "" {
			price := *saved.Pricing
			p := providers.Options{ID: saved.ProviderRef, Protocol: providers.Protocol(saved.Protocol), BaseURL: saved.BaseURL, Model: saved.ModelID, APIKey: configured.APIKey, Pricing: &price, ContextWindowTokens: saved.ContextWindowTokens, Vision: saved.Vision}
			if p.Protocol != providers.ProtocolOpenAI && p.Protocol != providers.ProtocolAnthropic {
				return providers.Options{}, errors.New("unsupported saved protocol")
			}
			if err := p.NormalizeAndValidate(); err != nil {
				return providers.Options{}, errors.New("invalid saved model configuration")
			}
			return p, nil
		}
	}
	return providers.Options{}, errors.New("version credential reference unavailable")
}

// MCPConfigReference identifies server-controlled effective transport settings.
// Environment references stay symbolic; resolved credentials never enter it.
func MCPConfigReference(server MCPServerConfig) string {
	encoded, _ := json.Marshal(server)
	return fmt.Sprintf("mcp-config-v1:sha256:%x", sha256.Sum256(encoded))
}
func (c *Config) ResolveAgentMCP(ref agentversion.MCPRef) (pimcp.ServerOptions, error) {
	if ref.ImplementationRef != "mcp:v1" {
		return pimcp.ServerOptions{}, errors.New("MCP implementation unavailable")
	}
	for _, server := range c.MCP.Servers {
		if server.Enabled && server.Name == ref.Name && MCPConfigReference(server) == ref.ConfigRef {
			if !slices.Equal(server.AllowTools, ref.AllowTools) || server.ToolPrefix != ref.ToolPrefix {
				return pimcp.ServerOptions{}, errors.New("MCP policy mismatch")
			}
			for _, value := range server.Env {
				if !strings.HasPrefix(value, "${") {
					return pimcp.ServerOptions{}, errors.New("versioned MCP environment must use server credential references")
				}
			}
			if server.CWD != "" {
				return pimcp.ServerOptions{}, errors.New("versioned MCP cannot use a host working directory")
			}
			return (&MCPConfig{Servers: []MCPServerConfig{server}}).ServerOptions()[0], nil
		}
	}
	return pimcp.ServerOptions{}, errors.New("MCP configuration reference unavailable")
}
