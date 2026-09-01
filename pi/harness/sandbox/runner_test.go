package sandbox

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestBuildSandboxPayloadEnvRejectsContractOverride(t *testing.T) {
	for _, key := range []string{"PATH", "HOME", "TMPDIR"} {
		_, err := BuildSandboxPayloadEnv("/ws", "/tmp", []string{key + "=/evil"})
		if !errors.Is(err, ErrEnvContractRejected) {
			t.Fatalf("contract key %s: error = %v, want ErrEnvContractRejected", key, err)
		}
	}
}

func TestBuildSandboxPayloadEnvMergesAndSorts(t *testing.T) {
	env, err := BuildSandboxPayloadEnv("/ws", "/tmp", []string{
		"B=2", "A=1", "TERM=xterm-256color", // LANG/TERM 非契约：可覆盖，后写胜出
	})
	if err != nil {
		t.Fatalf("BuildSandboxPayloadEnv() error = %v", err)
	}
	if !slices.IsSorted(env) {
		t.Fatalf("env 未排序: %v", env)
	}
	merged := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		merged[k] = v
	}
	if merged["HOME"] != "/ws" || merged["TMPDIR"] != "/tmp" || merged["PATH"] != SandboxPath {
		t.Fatalf("基础项被破坏: %v", merged)
	}
	if merged["TERM"] != "xterm-256color" || merged["A"] != "1" || merged["B"] != "2" {
		t.Fatalf("外部输入未生效: %v", merged)
	}
}

func TestBuildSandboxPayloadEnvRejectsInvalidEntry(t *testing.T) {
	for _, entry := range []string{"NVALUE", "A=B\x00C", "=v"} {
		if _, err := BuildSandboxPayloadEnv("/ws", "/tmp", []string{entry}); err == nil {
			t.Fatalf("entry %q 应被拒绝", entry)
		}
	}
}

func TestValidateSandboxPayloadEnv(t *testing.T) {
	env, err := BuildSandboxPayloadEnv("/ws", "/tmp", nil)
	if err != nil {
		t.Fatalf("BuildSandboxPayloadEnv() error = %v", err)
	}
	if err := ValidateSandboxPayloadEnv(env, "/ws", "/tmp"); err != nil {
		t.Fatalf("ValidateSandboxPayloadEnv() error = %v", err)
	}
	broken := slices.Clone(env)
	broken[0] = "HOME=/elsewhere"
	if err := ValidateSandboxPayloadEnv(broken, "/ws", "/tmp"); !errors.Is(err, ErrEnvContractRejected) {
		t.Fatalf("值不匹配应被拒: %v", err)
	}
}
