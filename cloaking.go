package main

import (
	"regexp"
	"strings"
)

const opencodeIdentityPrefix = "You are OpenCode"

var (
	paragraphRemovalAnchors = []string{
		"github.com/anomalyco/opencode",
		"opencode.ai/docs",
	}
	textReplacements = []struct {
		match       string
		replacement string
	}{
		{
			match:       "if OpenCode honestly",
			replacement: "if the assistant honestly",
		},
		{
			match:       "Here is some useful information about the environment you are running in:",
			replacement: "Environment context you are running in:",
		},
	}
	paragraphSplitPattern = regexp.MustCompile("\\n\\n+")
)

func sanitizeSystemText(text string) string {
	if text == "" {
		return ""
	}

	paragraphs := paragraphSplitPattern.Split(text, -1)
	keptParagraphs := make([]string, 0, len(paragraphs))

	for _, paragraph := range paragraphs {
		if strings.Contains(paragraph, opencodeIdentityPrefix) {
			continue
		}

		containsRemovalAnchor := false
		for _, anchor := range paragraphRemovalAnchors {
			if strings.Contains(paragraph, anchor) {
				containsRemovalAnchor = true
				break
			}
		}
		if containsRemovalAnchor {
			continue
		}

		keptParagraphs = append(keptParagraphs, paragraph)
	}

	result := strings.Join(keptParagraphs, "\n\n")
	for _, replacement := range textReplacements {
		result = strings.ReplaceAll(result, replacement.match, replacement.replacement)
	}

	return strings.TrimSpace(result)
}
