package main

// Manual QA harness (Task 5).
//
// This test drives the EXACT code path the CLIProxyAPI host invokes for the
// `request.intercept_after` ABI method: it marshals a realistic opencode
// RequestInterceptRequest the way the host does (base64 Body), calls
// interceptAfter (the function main.go's cliproxyPluginCall dispatches to),
// unwraps the envelope, and prints + asserts the cloaked result.
//
// Run: go test -run TestHarnessOpencodeRealistic -v
// The captured stdout is the QA evidence artifact.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/tidwall/gjson"
)

func TestHarnessOpencodeRealistic(t *testing.T) {
	// A realistic opencode Anthropic Messages request body:
	//  - system[0]: the "You are OpenCode" identity paragraph (must be DROPPED)
	//  - system[1]: a help block with opencode GitHub + docs URLs (must be DROPPED)
	//  - system[2]: legitimate tool guidance (must be PRESERVED)
	//  - a real user turn (must survive, shifted after the injected pair)
	innerBody := `{
  "model": "claude-sonnet-4-5-20250929",
  "max_tokens": 8192,
  "system": [
    {"type": "text", "text": "You are OpenCode, an autonomous coding agent built by Anomaly.\n\nYou help users complete software engineering tasks efficiently."},
    {"type": "text", "text": "# Help & Feedback\n\nReport issues at github.com/anomalyco/opencode or read the guide at opencode.ai/docs for more."},
    {"type": "text", "text": "# Tool Usage\n\nUse the available tools to read and edit files. Prefer ripgrep for searching the codebase."}
  ],
  "messages": [
    {"role": "user", "content": [{"type": "text", "text": "Refactor the auth module to use JWT sessions."}]}
  ]
}`

	// Build the wire request exactly as the host serializes it (Body is []byte
	// so json.Marshal base64-encodes it; interceptAfter decodes it back).
	req := pluginapi.RequestInterceptRequest{
		SourceFormat: "claude",
		ToFormat:     "claude",
		Model:        "claude-sonnet-4-5-20250929",
		Stream:       true,
		Headers: http.Header{
			"User-Agent": {"opencode/1.14.30 (darwin; arm64)"},
		},
		Body: []byte(innerBody),
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	// Invoke the real ABI-dispatched function.
	respRaw, err := interceptAfter(raw)
	if err != nil {
		t.Fatalf("interceptAfter: %v", err)
	}

	// Unwrap: envelope{ok, result} -> RequestInterceptResponse -> Body.
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if errUnmarshal := json.Unmarshal(respRaw, &env); errUnmarshal != nil {
		t.Fatalf("unmarshal envelope: %v", errUnmarshal)
	}
	if !env.OK {
		t.Fatalf("envelope not ok: %s", respRaw)
	}
	var resp pluginapi.RequestInterceptResponse
	if errUnmarshal := json.Unmarshal(env.Result, &resp); errUnmarshal != nil {
		t.Fatalf("unmarshal response: %v", errUnmarshal)
	}
	if len(resp.Body) == 0 {
		t.Fatalf("plugin returned empty body (no transform) — expected a cloaked request")
	}
	out := resp.Body

	// ---- Human-readable evidence dump ----
	fmt.Println("================ OPENCODE-CLOAK QA HARNESS ================")
	fmt.Println("--- INPUT (client User-Agent) ---")
	fmt.Println("User-Agent: opencode/1.14.30 (darwin; arm64)")
	fmt.Println("\n--- INPUT system[] (opencode) ---")
	gjson.GetBytes([]byte(innerBody), "system").ForEach(func(i, blk gjson.Result) bool {
		fmt.Printf("  [%s] %s\n", i.String(), strings.ReplaceAll(blk.Get("text").String(), "\n", "\\n"))
		return true
	})
	fmt.Println("\n--- OUTPUT system[] (cloaked) ---")
	gjson.GetBytes(out, "system").ForEach(func(i, blk gjson.Result) bool {
		fmt.Printf("  [%s] %s\n", i.String(), blk.Get("text").String())
		return true
	})
	fmt.Println("\n--- OUTPUT messages[] (relocated + original) ---")
	gjson.GetBytes(out, "messages").ForEach(func(i, msg gjson.Result) bool {
		text := msg.Get("content.0.text").String()
		fmt.Printf("  [%s] role=%s text=%q\n", i.String(), msg.Get("role").String(), text)
		return true
	})
	fmt.Println("==========================================================")

	// ---- Assertions (the binary observable pass conditions) ----
	billing := gjson.GetBytes(out, "system.0.text").String()
	billingRe := regexp.MustCompile(`^x-anthropic-billing-header: cc_version=2\.1\.63\.[0-9a-f]{3}; cc_entrypoint=cli; cch=[0-9a-f]{5};$`)
	if !billingRe.MatchString(billing) {
		t.Fatalf("system[0] billing header malformed: %q", billing)
	}
	if got := gjson.GetBytes(out, "system.1.text").String(); got != claudeCodeSystemPrompt {
		t.Fatalf("system[1] identity mismatch: %q", got)
	}
	if n := len(gjson.GetBytes(out, "system").Array()); n != 2 {
		t.Fatalf("expected system to have exactly 2 blocks, got %d", n)
	}

	msg0Role := gjson.GetBytes(out, "messages.0.role").String()
	msg0Text := gjson.GetBytes(out, "messages.0.content.0.text").String()
	if msg0Role != "user" || !strings.HasPrefix(msg0Text, "[System Instructions - follow these strictly]\n") {
		t.Fatalf("messages[0] is not the injected instruction user turn: role=%q text=%q", msg0Role, msg0Text)
	}
	if gjson.GetBytes(out, "messages.1.role").String() != "assistant" {
		t.Fatalf("messages[1] should be the assistant ack")
	}

	outStr := string(out)
	// Surgical PRESERVATION: legitimate tool guidance survived in the relocated prompt.
	if !strings.Contains(msg0Text, "Use the available tools to read and edit files") {
		t.Fatalf("sanitized prompt lost the preserved tool-usage guidance")
	}
	// Brand markers STRIPPED everywhere.
	for _, banned := range []string{"You are OpenCode", "github.com/anomalyco/opencode", "opencode.ai/docs"} {
		if strings.Contains(outStr, banned) {
			t.Fatalf("brand marker leaked into cloaked request: %q", banned)
		}
	}
	// The original user task must still be present (shifted after the injected pair).
	if !strings.Contains(outStr, "Refactor the auth module to use JWT sessions.") {
		t.Fatalf("original user message was lost")
	}

	fmt.Println("RESULT: PASS — billing header well-formed, opencode brand stripped, tool guidance preserved, original turn intact.")
}
