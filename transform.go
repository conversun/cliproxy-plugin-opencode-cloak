package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// resolvedConfig is the compiled, ready-to-use plugin configuration.
type resolvedConfig struct {
	version    string
	entrypoint string
	opencodeUA *regexp.Regexp
}

// currentConfig holds the active resolved configuration. It is replaced
// atomically by configure on plugin.register / plugin.reconfigure.
var currentConfig atomic.Pointer[resolvedConfig]

const claudeCodeSystemPrompt = "You are Claude Code, Anthropic's official CLI for Claude."

// defaultConfig returns the built-in configuration used before any host
// configuration is applied or when a user-supplied value is invalid.
func defaultConfig() resolvedConfig {
	return resolvedConfig{
		version:    "2.1.63",
		entrypoint: "cli",
		opencodeUA: regexp.MustCompile("(?i)^opencode/"),
	}
}

// loadConfig returns the active resolved configuration, or the defaults when
// none has been stored yet.
func loadConfig() resolvedConfig {
	if cfg := currentConfig.Load(); cfg != nil {
		return *cfg
	}
	return defaultConfig()
}

// interceptBefore rewrites an execution request before credential selection.
// No-op: all cloaking happens after auth selection, in interceptAfter.
func interceptBefore(raw []byte) ([]byte, error) {
	return okEnvelope(pluginapi.RequestInterceptResponse{})
}

// interceptAfter rewrites an execution request after credential selection.
func interceptAfter(raw []byte) (result []byte, err error) {
	defer func() {
		if recover() != nil {
			result, err = okEnvelope(pluginapi.RequestInterceptResponse{})
		}
	}()
	var req pluginapi.RequestInterceptRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return okEnvelope(pluginapi.RequestInterceptResponse{})
	}
	body, ok := transformInterceptAfter(req, loadConfig())
	if !ok {
		return okEnvelope(pluginapi.RequestInterceptResponse{})
	}
	return okEnvelope(pluginapi.RequestInterceptResponse{Body: body})
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func headerGet(headers http.Header, key string) string {
	for name, values := range headers {
		if strings.EqualFold(name, key) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// transformInterceptAfter aligns opencode requests to the current
// opencode-anthropic-auth system layout: billing header, Claude Code identity,
// then the sanitized original system blocks. The original conversation remains
// untouched. CLIProxyAPI still owns user-id injection, outgoing headers, and
// final-body cch re-signing.
func transformInterceptAfter(req pluginapi.RequestInterceptRequest, cfg resolvedConfig) (result []byte, transformed bool) {
	defer func() {
		if recover() != nil {
			result, transformed = nil, false
		}
	}()

	if req.SourceFormat != "claude" || req.ToFormat != "claude" || !gjson.ValidBytes(req.Body) {
		return nil, false
	}

	system := gjson.GetBytes(req.Body, "system")

	// Idempotent: never touch a request whose system already carries the billing
	// header (native already ran, or a prior pass transformed this body).
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(firstSystemText(system))), "x-anthropic-billing-header") {
		return nil, false
	}

	userAgent := headerGet(req.Headers, "User-Agent")
	// A genuine Claude Code request already looks correct — leave it alone.
	if extractClaudeCodeVersionFromUserAgent(userAgent) != "" && systemContainsText(system, claudeCodeSystemPrompt) {
		return nil, false
	}

	model := req.Model
	if model == "" {
		model = req.RequestedModel
	}
	// Native cloaking skips system injection for haiku; mirror that so we never
	// strip the prompt on a request native will leave uncloaked.
	if strings.HasPrefix(strings.ToLower(model), "claude-3-5-haiku") {
		return nil, false
	}

	originalSystemText := systemText(system)
	userAgentMatches := userAgent != "" && cfg.opencodeUA != nil && cfg.opencodeUA.MatchString(userAgent)
	if !userAgentMatches && !strings.Contains(originalSystemText, opencodeIdentityPrefix) {
		return nil, false
	}

	messages := gjson.GetBytes(req.Body, "messages")
	if !hasUserMessage(messages) {
		return nil, false
	}

	billing := buildBillingHeaderValue(messages, cfg.version, cfg.entrypoint)
	if billing == "" {
		return nil, false
	}

	blocks := []json.RawMessage{rawTextBlock(billing), rawTextBlock(claudeCodeSystemPrompt)}
	blocks = append(blocks, sanitizeSystemBlocks(system)...)
	encoded, errMarshal := json.Marshal(blocks)
	if errMarshal != nil {
		return nil, false
	}

	out := append([]byte(nil), req.Body...)
	updated, errSet := sjson.SetRawBytes(out, "system", encoded)
	if errSet != nil {
		return nil, false
	}
	return updated, true
}

func firstSystemText(system gjson.Result) string {
	if system.Type == gjson.String {
		return system.String()
	}
	if system.IsArray() {
		first := system.Get("0.text")
		if first.Type == gjson.String {
			return first.String()
		}
	}
	return ""
}

func systemContainsText(system gjson.Result, text string) bool {
	if system.Type == gjson.String {
		return strings.Contains(system.String(), text)
	}
	if !system.IsArray() {
		return false
	}
	contains := false
	system.ForEach(func(_, block gjson.Result) bool {
		if candidate := block.Get("text"); candidate.Type == gjson.String && strings.Contains(candidate.String(), text) {
			contains = true
			return false
		}
		return true
	})
	return contains
}

func systemText(system gjson.Result) string {
	if system.Type == gjson.String {
		return system.String()
	}
	if !system.IsArray() {
		return ""
	}
	parts := make([]string, 0)
	system.ForEach(func(_, block gjson.Result) bool {
		if text := block.Get("text"); text.Type == gjson.String {
			parts = append(parts, text.String())
		}
		return true
	})
	return strings.Join(parts, "\n\n")
}

func rawTextBlock(text string) json.RawMessage {
	raw, _ := json.Marshal(textBlock{Type: "text", Text: text})
	return raw
}

func sanitizeSystemBlocks(system gjson.Result) []json.RawMessage {
	if system.Type == gjson.String {
		if text := sanitizeSystemText(system.String()); text != "" {
			return []json.RawMessage{rawTextBlock(text)}
		}
		return nil
	}
	if !system.IsArray() {
		return nil
	}

	blocks := make([]json.RawMessage, 0, int(system.Get("#").Int()))
	system.ForEach(func(_, block gjson.Result) bool {
		text := block.Get("text")
		if text.Type != gjson.String {
			return true
		}
		sanitized := sanitizeSystemText(text.String())
		if sanitized == "" {
			return true
		}
		updated, errSet := sjson.Set(block.Raw, "text", sanitized)
		if errSet == nil {
			blocks = append(blocks, json.RawMessage(updated))
		}
		return true
	})
	return blocks
}

func hasUserMessage(messages gjson.Result) bool {
	if !messages.IsArray() {
		return false
	}
	found := false
	messages.ForEach(func(_, message gjson.Result) bool {
		if message.Get("role").String() == "user" {
			found = true
			return false
		}
		return true
	})
	return found
}
