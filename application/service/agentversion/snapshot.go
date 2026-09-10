// Package agentversion defines the immutable, credential-free runtime snapshot.
package agentversion

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/loopdetect"
)

var ErrInvalidSnapshot = errors.New("invalid agent version snapshot")

type ModelConfig struct {
	ProviderRef         string             `json:"provider_ref"`
	Protocol            string             `json:"protocol"`
	BaseURL             string             `json:"base_url"`
	ModelID             string             `json:"model_id"`
	Reasoning           string             `json:"reasoning"`
	ContextWindowTokens int64              `json:"context_window_tokens"`
	Vision              bool               `json:"vision"`
	Pricing             *providers.Pricing `json:"pricing"`
	Capabilities        []string           `json:"capabilities"`
	SecretRef           string             `json:"secret_ref"`
}

type BuiltinTools struct {
	Read, Write, Exec, Subagent bool
}

func (b BuiltinTools) MarshalJSON() ([]byte, error) {
	type wire struct {
		Read     bool `json:"read"`
		Write    bool `json:"write"`
		Exec     bool `json:"exec"`
		Subagent bool `json:"subagent"`
	}
	return json.Marshal(wire{b.Read, b.Write, b.Exec, b.Subagent})
}
func (b *BuiltinTools) UnmarshalJSON(data []byte) error {
	type wire struct {
		Read     *bool `json:"read"`
		Write    *bool `json:"write"`
		Exec     *bool `json:"exec"`
		Subagent *bool `json:"subagent"`
	}
	var w wire
	if err := strictDecode(data, &w); err != nil {
		return err
	}
	if w.Read == nil || w.Write == nil || w.Exec == nil || w.Subagent == nil {
		return fmt.Errorf("%w: incomplete builtin tool flags", ErrInvalidSnapshot)
	}
	b.Read, b.Write, b.Exec, b.Subagent = *w.Read, *w.Write, *w.Exec, *w.Subagent
	return nil
}

type ToolRef struct {
	Name              string            `json:"name"`
	ImplementationRef string            `json:"implementation_ref"`
	Arguments         map[string]string `json:"arguments"`
}
type MCPRef struct {
	Name              string   `json:"name"`
	ImplementationRef string   `json:"implementation_ref"`
	ConfigRef         string   `json:"config_ref"`
	AllowTools        []string `json:"allow_tools"`
	ToolPrefix        string   `json:"tool_prefix"`
}
type ToolPolicy struct {
	Builtin     BuiltinTools     `json:"builtin"`
	Registered  []ToolRef        `json:"registered"`
	MCP         []MCPRef         `json:"mcp"`
	Permissions []PermissionRule `json:"permissions"`
}
type PermissionRule struct {
	Tool     string   `json:"tool"`
	Patterns []string `json:"patterns"`
	Effect   string   `json:"effect"`
	Reason   string   `json:"reason"`
}
type CompactionConfig struct {
	ContextWindowTokens int64 `json:"context_window_tokens"`
	EnablePrune         bool  `json:"enable_prune"`
}
type WorkspacePolicy struct {
	WriteMode        string   `json:"write_mode"`
	WritablePrefixes []string `json:"writable_prefixes"`
}
type ToolRetryConfig struct {
	Attempts  int      `json:"attempts"`
	BackoffMS int      `json:"backoff_ms"`
	Tools     []string `json:"tools"`
}
type ToolRuntimeConfig struct {
	TimeoutSeconds int             `json:"timeout_seconds"`
	Retry          ToolRetryConfig `json:"retry"`
}
type RuntimeConfig struct {
	Limits              governor.Limits   `json:"limits"`
	LoopDetection       loopdetect.Config `json:"loop_detection"`
	Compaction          CompactionConfig  `json:"compaction"`
	HistoryMessageLimit int               `json:"history_message_limit"`
	WritePolicy         WorkspacePolicy   `json:"write_policy"`
	ToolRuntime         ToolRuntimeConfig `json:"tool_runtime"`
	AllowExec           bool              `json:"allow_exec"`
	AllowWrite          bool              `json:"allow_write"`
}
type Snapshot struct {
	SchemaVersion int           `json:"schema_version"`
	Model         ModelConfig   `json:"model"`
	Tools         ToolPolicy    `json:"tools"`
	Runtime       RuntimeConfig `json:"runtime"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}
type ValidationReport struct {
	Head                   string       `json:"head"`
	DigestVersion          int          `json:"digest_version"`
	BundleDigest           string       `json:"bundle_digest"`
	SpecDigest             string       `json:"spec_digest"`
	ValidatorVersion       string       `json:"validator_version"`
	ValidationPolicyDigest string       `json:"validation_policy_digest"`
	Passed                 bool         `json:"passed"`
	ReviewMode             string       `json:"review_mode"`
	Diagnostics            []Diagnostic `json:"diagnostics"`
	ValidatedAt            time.Time    `json:"validated_at"`
	ValidUntil             time.Time    `json:"valid_until"`
}

func ParseSnapshot(data []byte) (Snapshot, error) {
	type wire struct {
		SchemaVersion *int           `json:"schema_version"`
		Model         *ModelConfig   `json:"model"`
		Tools         *ToolPolicy    `json:"tools"`
		Runtime       *RuntimeConfig `json:"runtime"`
	}
	var w wire
	if err := DecodeStrict(data, &w); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	if err := requireSnapshotFields(data); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	if w.SchemaVersion == nil || w.Model == nil || w.Tools == nil || w.Runtime == nil {
		return Snapshot{}, fmt.Errorf("%w: required field missing", ErrInvalidSnapshot)
	}
	s := Snapshot{*w.SchemaVersion, *w.Model, *w.Tools, *w.Runtime}
	if err := s.validate(); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

func requireSnapshotFields(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	model, err := requiredObject(root, "model")
	if err != nil {
		return err
	}
	tools, err := requiredObject(root, "tools")
	if err != nil {
		return err
	}
	runtimeConfig, err := requiredObject(root, "runtime")
	if err != nil {
		return err
	}
	if err := requireKeys(root, "schema_version", "model", "tools", "runtime"); err != nil {
		return err
	}
	if err := requireKeys(model, "provider_ref", "protocol", "base_url", "model_id", "reasoning", "context_window_tokens", "vision", "pricing", "capabilities", "secret_ref"); err != nil {
		return err
	}
	pricing, err := requiredObject(model, "pricing")
	if err != nil {
		return err
	}
	if err := requireKeys(pricing, "input_usd_per_million_tokens", "output_usd_per_million_tokens"); err != nil {
		return err
	}
	builtin, err := requiredObject(tools, "builtin")
	if err != nil {
		return err
	}
	if err := requireKeys(tools, "builtin", "registered", "mcp", "permissions"); err != nil {
		return err
	}
	if err := requireKeys(builtin, "read", "write", "exec", "subagent"); err != nil {
		return err
	}
	if err := requireArrayObjects(tools["registered"], "name", "implementation_ref", "arguments"); err != nil {
		return err
	}
	if err := requireArrayObjects(tools["mcp"], "name", "implementation_ref", "config_ref", "allow_tools", "tool_prefix"); err != nil {
		return err
	}
	if err := requireArrayObjects(tools["permissions"], "tool", "patterns", "effect", "reason"); err != nil {
		return err
	}
	limits, err := requiredObject(runtimeConfig, "limits")
	if err != nil {
		return err
	}
	loop, err := requiredObject(runtimeConfig, "loop_detection")
	if err != nil {
		return err
	}
	compaction, err := requiredObject(runtimeConfig, "compaction")
	if err != nil {
		return err
	}
	writePolicy, err := requiredObject(runtimeConfig, "write_policy")
	if err != nil {
		return err
	}
	toolRuntime, err := requiredObject(runtimeConfig, "tool_runtime")
	if err != nil {
		return err
	}
	retry, err := requiredObject(toolRuntime, "retry")
	if err != nil {
		return err
	}
	if err := requireKeys(runtimeConfig, "limits", "loop_detection", "compaction", "history_message_limit", "write_policy", "tool_runtime", "allow_exec", "allow_write"); err != nil {
		return err
	}
	if err := requireKeys(limits, "max_turns", "max_cost_usd", "max_total_tokens"); err != nil {
		return err
	}
	if err := requireKeys(loop, "disabled", "excluded_tools"); err != nil {
		return err
	}
	if err := requireKeys(compaction, "context_window_tokens", "enable_prune"); err != nil {
		return err
	}
	if err := requireKeys(writePolicy, "write_mode", "writable_prefixes"); err != nil {
		return err
	}
	if err := requireKeys(toolRuntime, "timeout_seconds", "retry"); err != nil {
		return err
	}
	return requireKeys(retry, "attempts", "backoff_ms", "tools")
}

func requiredObject(parent map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	raw, ok := parent[key]
	if !ok {
		return nil, fmt.Errorf("missing %s", key)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return out, nil
}
func requireKeys(object map[string]json.RawMessage, keys ...string) error {
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return fmt.Errorf("missing %s", key)
		}
	}
	return nil
}
func requireArrayObjects(raw json.RawMessage, keys ...string) error {
	var values []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return errors.New("required array missing")
	}
	for _, value := range values {
		if err := requireKeys(value, keys...); err != nil {
			return err
		}
	}
	return nil
}

func (s Snapshot) validate() error {
	if s.SchemaVersion != 1 {
		return fmt.Errorf("%w: schema_version", ErrInvalidSnapshot)
	}
	if s.Model.ProviderRef == "" || s.Model.Protocol == "" || s.Model.BaseURL == "" || s.Model.ModelID == "" || s.Model.Reasoning != "" || s.Model.Pricing == nil || s.Model.Pricing.Validate() != nil || s.Model.SecretRef == "" || !strings.HasPrefix(s.Model.SecretRef, "platform:") || s.Model.Capabilities == nil {
		return fmt.Errorf("%w: model", ErrInvalidSnapshot)
	}
	if s.Tools.Registered == nil || s.Tools.MCP == nil || s.Tools.Permissions == nil || s.Runtime.HistoryMessageLimit <= 0 || s.Runtime.Compaction.ContextWindowTokens < 0 || s.Runtime.Limits.Validate() != nil || s.Runtime.AllowExec != s.Tools.Builtin.Exec || s.Runtime.AllowWrite != s.Tools.Builtin.Write {
		return fmt.Errorf("%w: runtime", ErrInvalidSnapshot)
	}
	if s.Runtime.WritePolicy.WriteMode != "all" && s.Runtime.WritePolicy.WriteMode != "restricted" {
		return fmt.Errorf("%w: write policy", ErrInvalidSnapshot)
	}
	for _, tool := range s.Tools.Registered {
		if tool.Name == "" || tool.ImplementationRef == "" || tool.Arguments == nil {
			return fmt.Errorf("%w: registered tool", ErrInvalidSnapshot)
		}
	}
	for _, mcp := range s.Tools.MCP {
		if mcp.Name == "" || mcp.ImplementationRef == "" || mcp.ConfigRef == "" || mcp.AllowTools == nil {
			return fmt.Errorf("%w: mcp", ErrInvalidSnapshot)
		}
	}
	for _, rule := range s.Tools.Permissions {
		if rule.Tool == "" || rule.Effect != "deny" || rule.Patterns == nil {
			return fmt.Errorf("%w: permission rule", ErrInvalidSnapshot)
		}
	}
	return nil
}

func strictDecode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

// DecodeStrict applies the platform JSON boundary rules before decoding a DTO.
func DecodeStrict(data []byte, dst any) error {
	if len(data) == 0 || len(data) > 1<<20 {
		return errors.New("JSON document size is invalid")
	}
	if err := validateJSONTree(data); err != nil {
		return err
	}
	return strictDecode(data, dst)
}

func validateJSONTree(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		if tok == nil {
			return errors.New("null is not allowed")
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for d.More() {
				keyTok, err := d.Token()
				if err != nil {
					return err
				}
				key := keyTok.(string)
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		case '[':
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		default:
			return errors.New("unexpected delimiter")
		}
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
