package main

import (
	"regexp"
	"strings"
)

// claudeCodeUAPattern matches canonical Claude Code User-Agents and captures
// the version and billing entrypoint.
var claudeCodeUAPattern = regexp.MustCompile(`(?i)^claude-cli/(\d{1,4}(?:\.\d{1,4}){1,3}) \(external, (cli|sdk-cli)\)$`)

type claudeCodeUserAgent struct {
	version    string
	entrypoint string
}

func parseClaudeCodeUserAgent(userAgent string) (claudeCodeUserAgent, bool) {
	match := claudeCodeUAPattern.FindStringSubmatch(strings.TrimSpace(userAgent))
	if match == nil {
		return claudeCodeUserAgent{}, false
	}
	return claudeCodeUserAgent{version: match[1], entrypoint: strings.ToLower(match[2])}, true
}

// extractClaudeCodeVersionFromUserAgent returns the version embedded in a
// canonical Claude Code User-Agent, or "" when the UA is not real Claude Code.
// It is used only as a gate: a real Claude Code request needs no cloaking.
func extractClaudeCodeVersionFromUserAgent(userAgent string) string {
	identity, ok := parseClaudeCodeUserAgent(userAgent)
	if !ok {
		return ""
	}
	return identity.version
}
