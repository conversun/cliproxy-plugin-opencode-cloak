package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/tidwall/gjson"
)

func TestCCH_returnsKnownHash_whenMessageTextProvided(t *testing.T) {
	// Given
	const text = "hello world test message"

	// When
	got := computeCCH(text)

	// Then
	const want = "4ffc3"
	if got != want {
		t.Fatalf("computeCCH(%q) = %q, want %q", text, got, want)
	}
}

func TestCCH_returnsDeterministicLowercaseHex_whenMessageTextProvided(t *testing.T) {
	// Given
	const text = "deterministic message"

	// When
	first := computeCCH(text)
	second := computeCCH(text)

	// Then
	if first != second {
		t.Fatalf("computeCCH(%q) changed between calls: %q then %q", text, first, second)
	}
	if !regexp.MustCompile(`^[0-9a-f]{5}$`).MatchString(first) {
		t.Fatalf("computeCCH(%q) = %q, want five lowercase hex characters", text, first)
	}
}

func TestVersionSuffix_returnsKnownHash_whenMessageAndVersionProvided(t *testing.T) {
	// Given
	const (
		text    = "hello world test message"
		version = "2.1.87"
	)

	// When
	got := computeVersionSuffix(text, version)

	// Then
	const want = "6ff"
	if got != want {
		t.Fatalf("computeVersionSuffix(%q, %q) = %q, want %q", text, version, got, want)
	}
}

func TestVersionSuffix_usesZeroForOutOfBoundsPositions_whenTextIsShort(t *testing.T) {
	// Given
	const (
		text    = "abc"
		version = "2.1.87"
	)
	sum := sha256.Sum256([]byte("59cf53e54c78" + "000" + version))
	want := hex.EncodeToString(sum[:])[:3]

	// When
	got := computeVersionSuffix(text, version)

	// Then
	if got != want {
		t.Fatalf("computeVersionSuffix(%q, %q) = %q, want %q", text, version, got, want)
	}
}

func TestVersionSuffix_usesUTF16CodeUnits_whenEmojiPrecedesSamplePositions(t *testing.T) {
	// Given
	const (
		text    = "😀abcDEFGHIJKLMNOPQRST"
		version = "2.1.87"
	)
	units := utf16.Encode([]rune(text))
	var chars strings.Builder
	for _, position := range [3]int{4, 7, 20} {
		if position < len(units) {
			chars.WriteString(string(utf16.Decode([]uint16{units[position]})))
		} else {
			chars.WriteString("0")
		}
	}
	sum := sha256.Sum256([]byte("59cf53e54c78" + chars.String() + version))
	want := hex.EncodeToString(sum[:])[:3]

	// When
	got := computeVersionSuffix(text, version)

	// Then
	if got != want {
		t.Fatalf("computeVersionSuffix(%q, %q) = %q, want UTF-16 sampled hash %q", text, version, got, want)
	}
}

func TestVersionSuffix_returnsEmpty_whenVersionIsBlank(t *testing.T) {
	// Given
	const text = "abc"

	for _, version := range []string{"", " \t\n"} {
		t.Run("version="+regexp.QuoteMeta(version), func(t *testing.T) {
			// When
			got := computeVersionSuffix(text, version)

			// Then
			if got != "" {
				t.Fatalf("computeVersionSuffix(%q, %q) = %q, want empty string", text, version, got)
			}
		})
	}
}

func TestVersionSuffix_returnsDeterministicLowercaseHex_whenVersionProvided(t *testing.T) {
	// Given
	const (
		text    = "deterministic message"
		version = "2.1.87"
	)

	// When
	first := computeVersionSuffix(text, version)
	second := computeVersionSuffix(text, version)

	// Then
	if first != second {
		t.Fatalf("computeVersionSuffix(%q, %q) changed between calls: %q then %q", text, version, first, second)
	}
	if !regexp.MustCompile(`^[0-9a-f]{3}$`).MatchString(first) {
		t.Fatalf("computeVersionSuffix(%q, %q) = %q, want three lowercase hex characters", text, version, first)
	}
}

func TestUserAgent_extractsVersion_whenUserAgentIsCanonicalClaudeCode(t *testing.T) {
	// Given
	const userAgent = "claude-cli/2.1.87 (external, cli)"

	// When
	got := extractClaudeCodeVersionFromUserAgent(userAgent)

	// Then
	const want = "2.1.87"
	if got != want {
		t.Fatalf("extractClaudeCodeVersionFromUserAgent(%q) = %q, want %q", userAgent, got, want)
	}
}

func TestUserAgent_returnsEmpty_whenUserAgentIsNotCanonicalClaudeCode(t *testing.T) {
	invalidUserAgents := []string{
		"claude-cli/2.1.0-beta.1 (external, cli)",
		"claude-cli/2.1.87;cc_entrypoint=attacker (external, cli)",
		"opencode/1.0",
		"claude-cli/2.1.87",
		"",
		" \t\n",
	}

	for _, userAgent := range invalidUserAgents {
		t.Run(regexp.QuoteMeta(userAgent), func(t *testing.T) {
			// When
			got := extractClaudeCodeVersionFromUserAgent(userAgent)

			// Then
			if got != "" {
				t.Fatalf("extractClaudeCodeVersionFromUserAgent(%q) = %q, want empty string", userAgent, got)
			}
		})
	}
}

func TestFirstUser_extractsTextAccordingToClaudeContentRules(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "skips leading non-text block",
			raw:  `[{"role":"user","content":[{"type":"image","source":{}},{"type":"text","text":"first text"}]}]`,
			want: "first text",
		},
		{
			name: "returns string content",
			raw:  `[{"role":"user","content":"string content"}]`,
			want: "string content",
		},
		{
			name: "uses first user message",
			raw:  `[{"role":"user","content":"first"},{"role":"user","content":"second"}]`,
			want: "first",
		},
		{
			name: "uses first text block",
			raw:  `[{"role":"user","content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]}]`,
			want: "first",
		},
		{
			name: "does not skip empty first text block",
			raw:  `[{"role":"user","content":[{"type":"text","text":""},{"type":"text","text":"later"}]}]`,
			want: "",
		},
		{
			name: "returns empty when first user has no text",
			raw:  `[{"role":"user","content":[{"type":"image"}]},{"role":"user","content":"later"}]`,
			want: "",
		},
		{
			name: "returns empty without user message",
			raw:  `[{"role":"assistant","content":"assistant"}]`,
			want: "",
		},
		{
			name: "returns empty for empty array",
			raw:  `[]`,
			want: "",
		},
		{
			name: "returns empty for non-array",
			raw:  `{"role":"user","content":"not an array"}`,
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			messages := gjson.Parse(test.raw)

			// When
			got := extractFirstUserMessageText(messages)

			// Then
			if got != test.want {
				t.Fatalf("extractFirstUserMessageText(%s) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestBilling_buildsGoldenHeader_whenInputsAreValid(t *testing.T) {
	// Given
	messages := gjson.Parse(`[{"role":"user","content":"hello world test message"}]`)

	// When
	got := buildBillingHeaderValue(messages, "2.1.87", "sdk-cli")

	// Then
	const want = "x-anthropic-billing-header: cc_version=2.1.87.6ff; cc_entrypoint=sdk-cli; cch=4ffc3;"
	if got != want {
		t.Fatalf("buildBillingHeaderValue() = %q, want %q", got, want)
	}
}

func TestBilling_returnsEmpty_whenVersionOrEntrypointIsInvalid(t *testing.T) {
	// Given
	messages := gjson.Parse(`[{"role":"user","content":"hello world test message"}]`)
	tests := []struct {
		name       string
		version    string
		entrypoint string
	}{
		{
			name:       "version injection",
			version:    "2.1.87;cc_entrypoint=attacker",
			entrypoint: "sdk-cli",
		},
		{
			name:       "entrypoint injection",
			version:    "2.1.87",
			entrypoint: "sdk-cli; cch=attacker",
		},
		{
			name:       "empty version",
			version:    "",
			entrypoint: "sdk-cli",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			got := buildBillingHeaderValue(messages, test.version, test.entrypoint)

			// Then
			if got != "" {
				t.Fatalf("buildBillingHeaderValue(version=%q, entrypoint=%q) = %q, want empty string", test.version, test.entrypoint, got)
			}
		})
	}
}

func TestVersionSuffix_recombinesSplitSurrogatePair_whenSampledUnitsAreAdjacentHalves(t *testing.T) {
	// UTF-16 positions of this text: 4 = high surrogate of the first emoji,
	// 7 = low surrogate of the SECOND emoji, 20 = 'x'. JavaScript concatenates
	// raw code units (text[4]+text[7]+text[20]), so the sampled high+low
	// surrogates recombine into U+1F601 before the UTF-8 hash. The reference
	// sampled string is therefore "\U0001F601x", NOT two U+FFFD replacements.
	text := "abcd" + string(rune(0x1F600)) + string(rune(0x1F601)) + "xxxxxxxxxxxxx"

	sum := sha256.Sum256([]byte(cchSalt + string(rune(0x1F601)) + "x" + "2.1.87"))
	want := hex.EncodeToString(sum[:])[:3]

	if got := computeVersionSuffix(text, "2.1.87"); got != want {
		t.Fatalf("surrogate-pair recombination parity: got %q want %q", got, want)
	}
}
