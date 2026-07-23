package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/tidwall/gjson"
)

var billingHeaderPattern = regexp.MustCompile(`^x-anthropic-billing-header: cc_version=2\.1\.63\.[0-9a-f]{3}; cc_entrypoint=cli; cch=[0-9a-f]{5};$`)

func TestTransformInterceptAfter_relocatesSystem_whenOpencodeUserAgentMatches(t *testing.T) {
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
	if billing := got.Get("system.0.text").String(); !billingHeaderPattern.MatchString(billing) {
		t.Fatalf("billing system block = %q, want canonical billing header", billing)
	}
	if identity := got.Get("system.1.text").String(); identity != claudeCodeSystemPrompt {
		t.Fatalf("Claude Code system block = %q, want %q", identity, claudeCodeSystemPrompt)
	}
	if role := got.Get("messages.0.role").String(); role != "user" {
		t.Fatalf("injected first role = %q, want user", role)
	}
	injected := got.Get("messages.0.content.0.text").String()
	if !strings.HasPrefix(injected, "[System Instructions - follow these strictly]\n") ||
		!strings.Contains(injected, "Keep this system instruction.") {
		t.Fatalf("injected system message = %q, want sanitized system instructions", injected)
	}
	if role := got.Get("messages.1.role").String(); role != "assistant" {
		t.Fatalf("injected second role = %q, want assistant", role)
	}
	if original := got.Get("messages.2.content").String(); original != "original user message" {
		t.Fatalf("relocated original message = %q, want original user message", original)
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
	injected := gjson.GetBytes(body, "messages.0.content.0.text").String()
	if strings.Contains(injected, "You are OpenCode") || !strings.Contains(injected, "Keep this benign paragraph.") {
		t.Fatalf("relocated system instructions = %q, want identity removed and benign paragraph preserved", injected)
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

func TestTransformInterceptAfter_addsUserMessage_whenSanitizedSystemAndNoMessages(t *testing.T) {
	// Given
	req := newTransformRequest(
		`{"system":"You are OpenCode, a coding agent.\n\nKeep this instruction.","messages":[]}`,
		nil,
	)

	// When
	body, ok := transformInterceptAfter(req, defaultConfig())

	// Then
	if !ok {
		t.Fatal("transformInterceptAfter() did not transform a request whose relocated system creates a user message")
	}
	if role := gjson.GetBytes(body, "messages.0.role").String(); role != "user" {
		t.Fatalf("first message role = %q, want user", role)
	}
}

func TestTransformInterceptAfter_skipsWhenNoUserRemainsAfterEmptySanitization(t *testing.T) {
	// Given
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

func TestTransformInterceptAfter_usesFirstUserContentForBilling_whenSystemIsEmpty(t *testing.T) {
	tests := []struct {
		name          string
		messages      string
		firstUserText string
	}{
		{
			name:          "string content",
			messages:      `[{"role":"user","content":"string user"}]`,
			firstUserText: "string user",
		},
		{
			name:          "mixed image and text blocks",
			messages:      `[{"role":"user","content":[{"type":"image","source":{}},{"type":"text","text":"text user"}]}]`,
			firstUserText: "text user",
		},
		{
			name:          "tool result only",
			messages:      `[{"role":"user","content":[{"type":"tool_result","content":"tool output"}]}]`,
			firstUserText: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			req := newTransformRequest(
				`{"system":"","messages":`+test.messages+`}`,
				http.Header{"User-Agent": []string{"opencode/1.14.3"}},
			)

			// When
			body, ok := transformInterceptAfter(req, defaultConfig())

			// Then
			if !ok {
				t.Fatal("transformInterceptAfter() did not transform an opencode request")
			}
			billing := gjson.GetBytes(body, "system.0.text").String()
			if want := "cch=" + computeCCH(test.firstUserText) + ";"; !strings.Contains(billing, want) {
				t.Fatalf("billing = %q, want first-user CCH %q", billing, want)
			}
			if role := gjson.GetBytes(body, "messages.0.role").String(); role != "user" {
				t.Fatalf("first original message role = %q, want user", role)
			}
		})
	}
}

func TestTransformInterceptAfter_appendsValidWorkloadOnly(t *testing.T) {
	tests := []struct {
		name             string
		workloadHeader   string
		containsWorkload bool
	}{
		{name: "valid", workloadHeader: "agent", containsWorkload: true},
		{name: "absent", workloadHeader: "", containsWorkload: false},
		{name: "invalid", workloadHeader: "a b;", containsWorkload: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			headers := http.Header{"User-Agent": []string{"opencode/1.14.3"}}
			if test.workloadHeader != "" {
				headers["x-cpa-claude-workload"] = []string{test.workloadHeader}
			}
			req := newTransformRequest(
				`{"system":"","messages":[{"role":"user","content":"hello"}]}`,
				headers,
			)

			// When
			body, ok := transformInterceptAfter(req, defaultConfig())

			// Then
			if !ok {
				t.Fatal("transformInterceptAfter() did not transform an opencode request")
			}
			billing := gjson.GetBytes(body, "system.0.text").String()
			if test.containsWorkload && !strings.HasSuffix(billing, " cc_workload=agent;") {
				t.Fatalf("billing = %q, want valid workload suffix", billing)
			}
			if !test.containsWorkload && strings.Contains(billing, "cc_workload=") {
				t.Fatalf("billing = %q, want no workload suffix", billing)
			}
		})
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
