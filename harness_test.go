package main

// Manual QA harness.
//
// Drives the EXACT code path the CLIProxyAPI host invokes for the
// `request.intercept_after` ABI method: it marshals a realistic opencode
// RequestInterceptRequest the way the host does (Body as []byte), calls
// interceptAfter (the function main.go's cliproxyPluginCall dispatches to),
// unwraps the envelope, and prints + asserts the delegated result.
//
// Current contract: the plugin emits billing, Claude Code identity, and the
// sanitized original system blocks. The original conversation is unchanged.
//
// Run: go test -run TestHarnessOpencodeRealistic -v
// The captured stdout is the QA evidence artifact.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/tidwall/gjson"
)

func TestHarnessOpencodeRealistic(t *testing.T) {
	// A realistic opencode Anthropic Messages request body:
	//  - system[0]: the "You are OpenCode" identity paragraph (must be DROPPED)
	//  - system[1]: a help block with opencode GitHub + docs URLs (must be DROPPED)
	//  - system[2]: legitimate tool guidance (must be PRESERVED in messages)
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

	// Build the wire request exactly as the host serializes it.
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
		t.Fatalf("plugin returned empty body (no transform) — expected a delegated request")
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
	fmt.Println("\n--- OUTPUT system[] (billing + identity + sanitized originals) ---")
	gjson.GetBytes(out, "system").ForEach(func(i, block gjson.Result) bool {
		fmt.Printf("  [%s] %s\n", i.String(), strings.ReplaceAll(block.Get("text").String(), "\n", "\\n"))
		return true
	})
	fmt.Println("\n--- OUTPUT messages[] (unchanged) ---")
	gjson.GetBytes(out, "messages").ForEach(func(i, msg gjson.Result) bool {
		text := msg.Get("content.0.text").String()
		fmt.Printf("  [%s] role=%s text=%q\n", i.String(), msg.Get("role").String(), text)
		return true
	})
	fmt.Println("==========================================================")

	// ---- Assertions (the binary observable pass conditions) ----
	if n := len(gjson.GetBytes(out, "system").Array()); n != 5 {
		t.Fatalf("expected billing + identity + 3 sanitized system blocks, got %d", n)
	}
	if !strings.HasPrefix(gjson.GetBytes(out, "system.0.text").String(), "x-anthropic-billing-header:") {
		t.Fatalf("system[0] must be the billing header")
	}
	if n := len(gjson.GetBytes(out, "messages").Array()); n != 1 {
		t.Fatalf("messages must remain unchanged, got %d entries", n)
	}

	outStr := string(out)
	if !strings.Contains(outStr, "Use the available tools to read and edit files") {
		t.Fatalf("sanitized prompt lost the preserved tool-usage guidance")
	}
	// Brand markers STRIPPED everywhere.
	for _, banned := range []string{"You are OpenCode", "github.com/anomalyco/opencode", "opencode.ai/docs"} {
		if strings.Contains(outStr, banned) {
			t.Fatalf("brand marker leaked into cloaked request: %q", banned)
		}
	}
	if !strings.Contains(outStr, "Refactor the auth module to use JWT sessions.") {
		t.Fatalf("original user message was lost")
	}

	fmt.Println("RESULT: PASS — billing/identity/system layout aligned, brand stripped, original conversation unchanged.")
}
