package config

import "testing"

func TestAgentPlatformDefaultsAndInvalidLimits(t *testing.T) {
	var c Config
	if err := c.normalizeAgentPlatform(); err != nil {
		t.Fatal(err)
	}
	if c.AgentRuntime.MaxInstances != 32 || c.AgentRuntime.MaxInstancesPerTenant != 8 || c.AgentRuntime.ValidationReservedInstances != 2 || c.AgentRuntime.ValidationReservedInstancesPerTenant != 1 || c.AgentTraining.SessionTTLSeconds != 86400 || c.AgentTraining.ValidationTTLSeconds != 1800 {
		t.Fatalf("unexpected defaults: %+v %+v", c.AgentRuntime, c.AgentTraining)
	}
	c.AgentRuntime.MaxInstances = 1
	if err := c.normalizeAgentPlatform(); err == nil {
		t.Fatal("invalid global limit accepted")
	}
	c = Config{}
	c.AgentTraining.SessionTTLSeconds = -1
	if err := c.normalizeAgentPlatform(); err == nil {
		t.Fatal("negative TTL accepted")
	}
}
