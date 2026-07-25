package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/tidwall/gjson"
)

func TestTransformInterceptAfter_preservesSanitizedSystemBlocks_whenOpencodeUserAgentMatches(t *testing.T) {
	// Given
	req := newTransformRequest(
		`{"system":[{"type":"text","text":"Keep this system instruction."}],"messages":[{"role":"user","content":"original user message"}]}`,
		http.Header{"user-agent": []string{"opencode/1.14.3"}},
	)

	// When
	body, ok := transformInterceptAfter(req, defaultConfig())

	// Then
	if !ok {
		t.Fatal("transformInterceptAfter() did not transform a matching opencode request")
	}
	got := gjson.ParseBytes(body)
	if n := len(got.Get("system").Array()); n != 3 {
		t.Fatalf("system block count = %d, want billing + identity + sanitized content", n)
	}
	if !strings.HasPrefix(got.Get("system.0.text").String(), "x-anthropic-billing-header:") {
		t.Fatalf("system[0] = %q, want billing header", got.Get("system.0.text").String())
	}
	if identity := got.Get("system.1.text").String(); identity != claudeCodeSystemPrompt {
		t.Fatalf("system[1] = %q, want Claude Code identity", identity)
	}
	if preserved := got.Get("system.2.text").String(); preserved != "Keep this system instruction." {
		t.Fatalf("system[2] = %q, want preserved sanitized instruction", preserved)
	}
	if n := len(got.Get("messages").Array()); n != 1 {
		t.Fatalf("messages count = %d, want original conversation unchanged", n)
	}
	if original := got.Get("messages.0.content").String(); original != "original user message" {
		t.Fatalf("original message = %q, want original user message", original)
	}
}

func TestTransformInterceptAfter_removesOpenCodeIdentity_whenSystemIdentityActivates(t *testing.T) {
	// Given
	req := newTransformRequest(
		`{"system":[{"type":"text","text":"You are OpenCode, a coding agent.\n\nKeep this benign paragraph."}],"messages":[{"role":"user","content":"original"}]}`,
		nil,
	)

	// When
	body, ok := transformInterceptAfter(req, defaultConfig())

	// Then
	if !ok {
		t.Fatal("transformInterceptAfter() did not transform a request with OpenCode system identity")
	}
	transformed := string(body)
	if strings.Contains(transformed, "You are OpenCode") || !strings.Contains(transformed, "Keep this benign paragraph.") {
		t.Fatalf("transformed request must remove identity and preserve benign system content: %s", transformed)
	}
}

func TestTransformInterceptAfter_preservesSystemBlockMetadata_whenSanitizingText(t *testing.T) {
	// Given
	req := newTransformRequest(
		`{"system":[{"type":"text","text":"Keep this instruction.","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hello"}]}`,
		http.Header{"User-Agent": []string{"opencode/1.14.3"}},
	)

	// When
	body, ok := transformInterceptAfter(req, defaultConfig())

	// Then
	if !ok {
		t.Fatal("transformInterceptAfter() did not transform a matching opencode request")
	}
	if cacheType := gjson.GetBytes(body, "system.2.cache_control.type").String(); cacheType != "ephemeral" {
		t.Fatalf("system block cache_control type = %q, want preserved ephemeral metadata", cacheType)
	}
}

func TestTransformInterceptAfter_skipsProtectedRequests(t *testing.T) {
	tests := []struct {
		name string
		req  pluginapi.RequestInterceptRequest
	}{
		{
			name: "real Claude Code",
			req: newTransformRequest(
				`{"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}],"messages":[{"role":"user","content":"hello"}]}`,
				http.Header{"User-Agent": []string{"claude-cli/2.1.87 (external, cli)"}},
			),
		},
		{
			name: "billing header already present",
			req: newTransformRequest(
				`{"system":[{"type":"text","text":"x-anthropic-billing-header: existing"}],"messages":[{"role":"user","content":"hello"}]}`,
				http.Header{"User-Agent": []string{"opencode/1.14.3"}},
			),
		},
		{
			name: "opencode URL alone",
			req: newTransformRequest(
				`{"system":"Read opencode.ai/docs for help.","messages":[{"role":"user","content":"hello"}]}`,
				http.Header{"User-Agent": []string{"CherryStudio/1.2"}},
			),
		},
		{
			name: "haiku model",
			req: func() pluginapi.RequestInterceptRequest {
				req := newTransformRequest(
					`{"system":"benign instruction","messages":[{"role":"user","content":"hello"}]}`,
					http.Header{"User-Agent": []string{"opencode/1.14.3"}},
				)
				req.Model = "claude-3-5-haiku-20241022"
				return req
			}(),
		},
		{
			name: "openai source format",
			req: func() pluginapi.RequestInterceptRequest {
				req := newTransformRequest(
					`{"system":"benign instruction","messages":[{"role":"user","content":"hello"}]}`,
					http.Header{"User-Agent": []string{"opencode/1.14.3"}},
				)
				req.SourceFormat = "openai"
				return req
			}(),
		},
		{
			name: "openai target format",
			req: func() pluginapi.RequestInterceptRequest {
				req := newTransformRequest(
					`{"system":"benign instruction","messages":[{"role":"user","content":"hello"}]}`,
					http.Header{"User-Agent": []string{"opencode/1.14.3"}},
				)
				req.ToFormat = "openai"
				return req
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			body, ok := transformInterceptAfter(test.req, defaultConfig())

			// Then
			if ok || body != nil {
				t.Fatalf("transformInterceptAfter() = (%q, %t), want (nil, false)", body, ok)
			}
		})
	}
}

func TestTransformInterceptAfter_skipsGenericClients_whenNoOpenCodeEvidence(t *testing.T) {
	for _, userAgent := range []string{"Cline/3.1", "CherryStudio/1.2", ""} {
		t.Run(userAgent, func(t *testing.T) {
			// Given
			req := newTransformRequest(
				`{"system":"benign instruction","messages":[{"role":"user","content":"hello"}]}`,
				http.Header{"User-Agent": []string{userAgent}},
			)

			// When
			body, ok := transformInterceptAfter(req, defaultConfig())

			// Then
			if ok || body != nil {
				t.Fatalf("transformInterceptAfter() = (%q, %t), want (nil, false)", body, ok)
			}
		})
	}
}

func TestTransformInterceptAfter_skipsWhenNoUserMessageExistsForBilling(t *testing.T) {
	// Given
	req := newTransformRequest(
		`{"system":"You are OpenCode, a coding agent.\n\nKeep this instruction.","messages":[]}`,
		nil,
	)

	// When
	body, ok := transformInterceptAfter(req, defaultConfig())

	// Then
	if ok || body != nil {
		t.Fatalf("transformInterceptAfter() = (%q, %t), want safe no-op without a user message", body, ok)
	}
}

func TestTransformInterceptAfter_skipsWhenNothingRemainsAfterSanitization(t *testing.T) {
	// Given: the entire system is opencode brand identity, which sanitizes to "".
	// Nothing to preserve -> no-op, and native cloaks the request normally.
	req := newTransformRequest(
		`{"system":"You are OpenCode, a coding agent.","messages":[{"role":"assistant","content":"hello"}]}`,
		nil,
	)

	// When
	body, ok := transformInterceptAfter(req, defaultConfig())

	// Then
	if ok || body != nil {
		t.Fatalf("transformInterceptAfter() = (%q, %t), want (nil, false)", body, ok)
	}
}

func newTransformRequest(body string, headers http.Header) pluginapi.RequestInterceptRequest {
	return pluginapi.RequestInterceptRequest{
		SourceFormat: "claude",
		ToFormat:     "claude",
		Headers:      headers,
		Body:         []byte(body),
	}
}
