package index

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractHashtags(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "simple hashtag",
			content: "This is #test content",
			want:    []string{"test"},
		},
		{
			name:    "multiple hashtags",
			content: "Working on #auth and #login features",
			want:    []string{"auth", "login"},
		},
		{
			name:    "hashtag at line start",
			content: "#feature request for navigation",
			want:    []string{"feature"},
		},
		{
			name:    "hyphenated hashtag",
			content: "This is #multi-word-tag content",
			want:    []string{"multi-word-tag"},
		},
		{
			name:    "hashtag with numbers",
			content: "Working on #auth2 system",
			want:    []string{"auth2"},
		},
		{
			name:    "skip markdown heading",
			content: "# Heading\n\nContent with #real tag",
			want:    []string{"real"},
		},
		{
			name:    "skip code block",
			content: "Text\n```go\n#ignored\n```\nMore #valid text",
			want:    []string{"valid"},
		},
		{
			name:    "skip inline code",
			content: "Use `#notthis` but #this is valid",
			want:    []string{"this"},
		},
		{
			name:    "deduplicate hashtags",
			content: "#auth login #auth again",
			want:    []string{"auth"},
		},
		{
			name:    "case insensitive",
			content: "#Auth and #AUTH and #auth",
			want:    []string{"auth"},
		},
		{
			name:    "no hashtags",
			content: "Plain content without tags",
			want:    nil,
		},
		{
			name:    "hashtag must start with letter",
			content: "#123 is not valid but #a123 is",
			want:    []string{"a123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractHashtags(tt.content)
			sort.Strings(got)
			sort.Strings(tt.want)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractHashtags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractWords(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "simple sentence",
			content: "Implement authentication feature",
			want:    []string{"implement", "authentication", "feature"},
		},
		{
			name:    "filters stop words",
			content: "The user should be able to login",
			want:    []string{"user", "able", "login"},
		},
		{
			name:    "filters short words",
			content: "A go to fix",
			want:    []string{"fix"},
		},
		{
			name:    "removes markdown",
			content: "# Heading\n\n**Bold** and *italic*",
			want:    []string{"heading", "bold", "italic"},
		},
		{
			name:    "handles links",
			content: "Check [the docs](http://example.com) for info",
			want:    []string{"check", "docs", "info"},
		},
		{
			name:    "skips code blocks",
			content: "Text\n```\ncode here\n```\nadditional text",
			want:    []string{"text", "additional", "text"},
		},
		{
			name:    "lowercase normalization",
			content: "UPPERCASE and MixedCase",
			want:    []string{"uppercase", "mixedcase"},
		},
		{
			name:    "handles underscores",
			content: "snake_case and kebab-case",
			want:    []string{"snake", "case", "kebab", "case"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractWords(tt.content)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractWords() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWordFrequency(t *testing.T) {
	content := "Feature request feature enhancement feature"
	freq := WordFrequency(content)

	if freq["feature"] != 3 {
		t.Errorf("frequency of 'feature' = %d, want 3", freq["feature"])
	}
	if freq["request"] != 1 {
		t.Errorf("frequency of 'request' = %d, want 1", freq["request"])
	}
	if freq["enhancement"] != 1 {
		t.Errorf("frequency of 'enhancement' = %d, want 1", freq["enhancement"])
	}
}
