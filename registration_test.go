package main

// Registration handshake regression test.
//
// CLIProxyAPI's host-side validPlugin() rejects a plugin (logging
// "returned invalid metadata or no capabilities") unless ALL of
// Metadata.Name/Version/Author/GitHubRepository are non-empty AND at least
// one capability is declared. A plugin that fails this still loads (dlopen
// succeeds) but never registers. This test drives the real
// plugin.register method and asserts that contract so an empty metadata
// field can never ship again.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
)

// storePluginVersionPattern mirrors pluginVersionPattern in CLIProxyAPI's
// internal/pluginstore/registry.go. The plugin store rejects any release whose
// version fails this check, so the registration metadata must satisfy it too.
var storePluginVersionPattern = regexp.MustCompile(`^[0-9][0-9A-Za-z.+-]*$`)

func TestPluginRegister_satisfiesHostValidPluginContract(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodPluginRegister, nil)
	if err != nil {
		t.Fatalf("plugin.register returned error: %v", err)
	}

	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil {
		t.Fatalf("unmarshal register envelope: %v", errUnmarshal)
	}
	if !env.OK {
		t.Fatalf("register envelope not ok: %s", raw)
	}

	var reg registration
	if errUnmarshal := json.Unmarshal(env.Result, &reg); errUnmarshal != nil {
		t.Fatalf("unmarshal registration: %v", errUnmarshal)
	}

	// Host validPlugin() requires every one of these to be non-empty.
	required := map[string]string{
		"Name":             reg.Metadata.Name,
		"Version":          reg.Metadata.Version,
		"Author":           reg.Metadata.Author,
		"GitHubRepository": reg.Metadata.GitHubRepository,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("Metadata.%s is empty; host validPlugin() would reject the plugin as invalid metadata", field)
		}
	}

	// The plugin store derives the version from the git release tag and validates
	// it against pluginVersionPattern (internal/pluginstore/registry.go). CI injects
	// the tag via -ldflags, so assert the shape rather than a literal: a hardcoded
	// version here would silently drift from the tag that actually ships.
	if !storePluginVersionPattern.MatchString(reg.Metadata.Version) {
		t.Fatalf("Metadata.Version = %q does not match plugin store pattern %s", reg.Metadata.Version, storePluginVersionPattern)
	}

	// Host validPlugin() also requires at least one declared capability.
	if !reg.Capabilities.RequestInterceptor {
		t.Fatalf("capabilities.request_interceptor must be true, else the host registers no capability")
	}

	configFields := make(map[string]bool, len(reg.Metadata.ConfigFields))
	for _, field := range reg.Metadata.ConfigFields {
		configFields[field.Name] = true
	}
	if !configFields["claude_code_user_agent"] {
		t.Fatal("registration must expose claude_code_user_agent")
	}
	if configFields["claude_code_version"] || configFields["entrypoint"] {
		t.Fatal("registration must not expose separately synchronized version or entrypoint fields")
	}

	// The plugin must not advertise a newer schema than the host supports.
	if reg.SchemaVersion > pluginabi.SchemaVersion {
		t.Fatalf("schema_version %d exceeds host SchemaVersion %d", reg.SchemaVersion, pluginabi.SchemaVersion)
	}
}
