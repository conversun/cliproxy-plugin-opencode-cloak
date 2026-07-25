package main

import (
	"regexp"
	"strings"
)

// claudeCodeUAPattern matches the canonical Claude Code CLI User-Agent, e.g.
// "claude-cli/2.1.87 (external, cli)". The captured group is the version.
var claudeCodeUAPattern = regexp.MustCompile(`(?i)^claude-cli/(\d{1,4}(?:\.\d{1,4}){1,3}) \(external, cli\)$`)

// extractClaudeCodeVersionFromUserAgent returns the version embedded in a
// canonical Claude Code User-Agent, or "" when the UA is not real Claude Code.
// It is used only as a gate: a real Claude Code request needs no cloaking.
func extractClaudeCodeVersionFromUserAgent(userAgent string) string {
	if strings.TrimSpace(userAgent) == "" {
		return ""
	}
	match := claudeCodeUAPattern.FindStringSubmatch(userAgent)
	if match == nil {
		return ""
	}
	return match[1]
}
