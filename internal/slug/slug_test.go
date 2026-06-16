package slug

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "simple", input: "Add dark mode toggle", want: "add-dark-mode-toggle"},
		{name: "unicode removed", input: "研究 OAuth2 providers", want: "oauth2-providers"},
		{name: "hyphen trim", input: "---leading and trailing---", want: "leading-and-trailing"},
		{name: "word limit", input: "one two three four five six", want: "one-two-three-four-five"},
		{name: "length limit", input: "supercalifragilisticexpialidocious is a very long word indeed", want: "supercalifragilisticexpialidocious-is-a-very-long"},
		{name: "special only", input: "!!!@@@", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Generate(tt.input); got != tt.want {
				t.Fatalf("Generate(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateLimits(t *testing.T) {
	got := Generate("this is a very long title that should be truncated to fifty characters")
	if len(got) > 50 {
		t.Fatalf("Generate() length = %d, want <= 50", len(got))
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("Generate() = %q, should not end with hyphen", got)
	}
}
