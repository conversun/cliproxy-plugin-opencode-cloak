package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/tidwall/gjson"
)

const cchSalt = "59cf53e54c78"

var (
	cchPositions        = [3]int{4, 7, 20}
	versionPattern      = regexp.MustCompile(`^\d{1,4}(?:\.\d{1,4}){1,3}$`)
	entrypointPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	claudeCodeUAPattern = regexp.MustCompile(`(?i)^claude-cli/(\d{1,4}(?:\.\d{1,4}){1,3}) \(external, cli\)$`)
)

func computeCCH(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:5]
}

func computeVersionSuffix(text, version string) string {
	if strings.TrimSpace(version) == "" {
		return ""
	}

	units := utf16.Encode([]rune(text))
	// Collect the sampled code units first, then decode ONCE. JavaScript builds
	// the sampled string by concatenating raw UTF-16 code units (text[i]), so a
	// sampled high surrogate immediately followed by a sampled low surrogate
	// recombines into a single code point before the UTF-8 hash. Decoding each
	// unit independently would instead emit two U+FFFD and diverge from JS.
	sampled := make([]uint16, 0, len(cchPositions))
	for _, position := range cchPositions {
		if position < len(units) {
			sampled = append(sampled, units[position])
			continue
		}
		sampled = append(sampled, uint16('0'))
	}
	chars := string(utf16.Decode(sampled))

	sum := sha256.Sum256([]byte(cchSalt + chars + version))
	return hex.EncodeToString(sum[:])[:3]
}

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

func extractFirstUserMessageText(messages gjson.Result) string {
	if !messages.IsArray() {
		return ""
	}

	var text string
	messages.ForEach(func(_, message gjson.Result) bool {
		if message.Get("role").String() != "user" {
			return true
		}

		content := message.Get("content")
		if content.Type == gjson.String {
			text = content.String()
			return false
		}
		if !content.IsArray() {
			return false
		}

		content.ForEach(func(_, block gjson.Result) bool {
			if block.Get("type").String() != "text" {
				return true
			}

			candidate := block.Get("text")
			if candidate.Type == gjson.String && candidate.String() != "" {
				text = candidate.String()
			}
			return false
		})
		return false
	})
	return text
}

func buildBillingHeaderValue(messages gjson.Result, version, entrypoint string) string {
	if strings.TrimSpace(version) == "" || !versionPattern.MatchString(version) ||
		strings.TrimSpace(entrypoint) == "" || !entrypointPattern.MatchString(entrypoint) {
		return ""
	}

	text := extractFirstUserMessageText(messages)
	suffix := computeVersionSuffix(text, version)
	cch := computeCCH(text)
	return "x-anthropic-billing-header: cc_version=" + version + "." + suffix +
		"; cc_entrypoint=" + entrypoint + "; cch=" + cch + ";"
}
