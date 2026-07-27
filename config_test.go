package main

import (
	"encoding/json"
	"testing"
)

func TestConfigure_extractsBillingIdentity_whenClaudeCodeUserAgentIsCanonical(t *testing.T) {
	// Given
	currentConfig.Store(nil)
	t.Cleanup(func() { currentConfig.Store(nil) })
	request, errMarshal := json.Marshal(lifecycleRequest{ConfigYAML: []byte(`claude_code_user_agent: "claude-cli/2.1.220 (external, sdk-cli)"`)})
	if errMarshal != nil {
		t.Fatalf("marshal lifecycle request: %v", errMarshal)
	}

	// When
	errConfigure := configure(request)

	// Then
	if errConfigure != nil {
		t.Fatalf("configure() error = %v", errConfigure)
	}
	if got := loadConfig().version; got != "2.1.220" {
		t.Fatalf("configured version = %q, want %q", got, "2.1.220")
	}
	if got := loadConfig().entrypoint; got != "sdk-cli" {
		t.Fatalf("configured entrypoint = %q, want %q", got, "sdk-cli")
	}
}

func TestConfigure_keepsDefaultVersion_whenClaudeCodeUserAgentIsInvalid(t *testing.T) {
	// Given
	currentConfig.Store(nil)
	t.Cleanup(func() { currentConfig.Store(nil) })
	request, errMarshal := json.Marshal(lifecycleRequest{ConfigYAML: []byte(`claude_code_user_agent: "claude-cli/2.1.220;cc_entrypoint=attacker (external, cli)"`)})
	if errMarshal != nil {
		t.Fatalf("marshal lifecycle request: %v", errMarshal)
	}

	// When
	errConfigure := configure(request)

	// Then
	if errConfigure != nil {
		t.Fatalf("configure() error = %v", errConfigure)
	}
	if got := loadConfig().version; got != defaultConfig().version {
		t.Fatalf("configured version = %q, want default %q", got, defaultConfig().version)
	}
}
