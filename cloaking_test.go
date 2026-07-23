package main

import "testing"

func TestSanitizeSystemText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "drops OpenCode identity paragraph",
			text: "You are OpenCode, a coding agent.\n\nKeep this paragraph.",
			want: "Keep this paragraph.",
		},
		{
			name: "drops paragraphs containing removal anchors",
			text: "Keep this paragraph.\n\nSee github.com/anomalyco/opencode for help.\n\nRead opencode.ai/docs for details.\n\nKeep this one too.",
			want: "Keep this paragraph.\n\nKeep this one too.",
		},
		{
			name: "replaces surviving OpenCode phrase",
			text: "The assistant should answer if OpenCode honestly cannot help.",
			want: "The assistant should answer if the assistant honestly cannot help.",
		},
		{
			name: "replaces environment context phrase",
			text: "Here is some useful information about the environment you are running in:",
			want: "Environment context you are running in:",
		},
		{
			name: "replaces every occurrence",
			text: "if OpenCode honestly cannot help, explain if OpenCode honestly why.",
			want: "if the assistant honestly cannot help, explain if the assistant honestly why.",
		},
		{
			name: "returns empty input unchanged",
			text: "",
			want: "",
		},
		{
			name: "returns empty when every paragraph is removed",
			text: "You are OpenCode, the agent.",
			want: "",
		},
		{
			name: "preserves unrelated paragraphs",
			text: "First unrelated paragraph.\n\nSecond unrelated paragraph.",
			want: "First unrelated paragraph.\n\nSecond unrelated paragraph.",
		},
		{
			name: "trims leading and trailing whitespace",
			text: " \n\tKeep this paragraph.\n ",
			want: "Keep this paragraph.",
		},
		{
			name: "drops all text when CRLF blocks remain one paragraph",
			text: "You are OpenCode, agent.\r\n\r\nSecond block.",
			want: "",
		},
		{
			name: "splits LF paragraphs",
			text: "You are OpenCode.\n\nSecond block.",
			want: "Second block.",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := sanitizeSystemText(testCase.text)

			if got != testCase.want {
				t.Fatalf("sanitizeSystemText() = %q, want %q", got, testCase.want)
			}
		})
	}
}
