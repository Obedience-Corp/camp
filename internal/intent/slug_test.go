package intent

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateSlug(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Normal cases
		{"simple title", "Add dark mode toggle", "add-dark-mode-toggle"},
		{"single word", "Refactor", "refactor"},
		{"two words", "Fix bug", "fix-bug"},
		{"three words", "Update readme file", "update-readme-file"},

		// Special characters
		{"colon and exclaim", "Fix: Login timeout!", "fix-login-timeout"},
		{"parentheses", "Add feature (beta)", "add-feature-beta"},
		{"ampersand", "Auth & Authorization", "auth-authorization"},
		{"quotes", `Say "hello"`, "say-hello"},
		{"single quotes", "Don't worry", "dont-worry"},
		{"brackets", "Add [beta] feature", "add-beta-feature"},
		{"at symbol", "email@example feature", "emailexample-feature"},
		{"hash symbol", "Issue #42 fix", "issue-42-fix"},
		{"asterisk", "Important* task", "important-task"},
		{"period", "file.txt support", "filetxt-support"},
		{"comma", "one, two, three", "one-two-three"},
		{"semicolon", "first; second", "first-second"},

		// Unicode and emoji
		{"unicode chars", "研究 OAuth2 providers", "oauth2-providers"},
		{"emoji in title", "Add 🎉 celebration feature", "add-celebration-feature"},
		{"mixed unicode", "Café ordering system", "caf-ordering-system"},
		{"emoji only", "🎉🎊🎈", ""},
		{"chinese only", "研究开发", ""},
		{"cyrillic", "Привет мир", ""},
		{"accented latin", "résumé créateur", "rsum-crateur"},

		// Whitespace handling
		{"multiple spaces", "Fix   login   issue", "fix-login-issue"},
		{"tabs", "Fix\tlogin\tissue", "fix-login-issue"},
		{"newline", "Fix\nlogin\nissue", "fix-login-issue"},
		{"leading trailing spaces", "  trim me  ", "trim-me"},
		{"mixed whitespace", " \t\nfix\t\n issue \t\n", "fix-issue"},

		// Hyphen handling
		{"existing hyphens", "pre-existing-hyphens", "pre-existing-hyphens"},
		{"multiple hyphens", "fix---login---issue", "fix-login-issue"},
		{"leading hyphens", "---leading", "leading"},
		{"trailing hyphens", "trailing---", "trailing"},
		{"mixed hyphens spaces", "fix - - login - - issue", "fix-login-issue"},

		// Length limits
		{"exactly 5 words", "one two three four five", "one-two-three-four-five"},
		{"more than 5 words", "one two three four five six seven", "one-two-three-four-five"},
		{"ten words", "one two three four five six seven eight nine ten", "one-two-three-four-five"},
		{"long words truncate", "supercalifragilisticexpialidocious is a very long word indeed", "supercalifragilisticexpialidocious-is-a-very-long"},

		// Edge cases
		{"empty string", "", ""},
		{"only special chars", "!!!@@@###$$$", ""},
		{"only spaces", "     ", ""},
		{"only hyphens", "---", ""},
		{"numbers only", "12345", "12345"},
		{"single letter", "a", "a"},
		{"single number", "1", "1"},
		{"single hyphen", "-", ""},

		// Real world examples
		{"bug report", "Fix: Crash when clicking save button", "fix-crash-when-clicking-save"},
		{"feature request", "Add OAuth2 support for Google", "add-oauth2-support-for-google"},
		{"research task", "Research: Best practices for API design", "research-best-practices-for-api"},
		{"chore", "Update dependencies to latest versions", "update-dependencies-to-latest-versions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateSlug(tt.input)
			if got != tt.want {
				t.Errorf("GenerateSlug(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateID(t *testing.T) {
	// Fixed timestamp for deterministic testing
	ts := time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC)

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"normal title", "Add dark mode toggle", "add-dark-mode-toggle-20260119-153412"},
		{"empty title", "", "20260119-153412"},
		{"special chars", "Fix: bug!", "fix-bug-20260119-153412"},
		{"unicode", "研究 OAuth2", "oauth2-20260119-153412"},
		{"emoji", "🎉 celebration", "celebration-20260119-153412"},
		{"long title", "one two three four five six seven", "one-two-three-four-five-20260119-153412"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateID(tt.title, ts)
			if got != tt.want {
				t.Errorf("GenerateID(%q, ts) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestGenerateID_DifferentTimestamps(t *testing.T) {
	title := "Test Title"

	tests := []struct {
		name string
		ts   time.Time
		want string
	}{
		{
			"midnight",
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			"test-title-20260101-000000",
		},
		{
			"noon",
			time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
			"test-title-20260615-120000",
		},
		{
			"end of day",
			time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			"test-title-20261231-235959",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateID(title, tt.ts)
			if got != tt.want {
				t.Errorf("GenerateID(%q, %v) = %q, want %q", title, tt.ts, got, tt.want)
			}
		})
	}
}

func TestGenerateSlug_LengthLimit(t *testing.T) {
	// Verify 50 char limit
	longTitle := "this is a very long title that should be truncated to fifty characters"
	slug := GenerateSlug(longTitle)

	if len(slug) > 50 {
		t.Errorf("GenerateSlug produced slug longer than 50 chars: %d", len(slug))
	}

	// Shouldn't end with hyphen
	if len(slug) > 0 && slug[len(slug)-1] == '-' {
		t.Errorf("GenerateSlug produced slug ending with hyphen: %q", slug)
	}
}

func TestGenerateSlug_WordLimit(t *testing.T) {
	// Verify 5 word limit
	manyWords := "one two three four five six seven eight nine ten"
	slug := GenerateSlug(manyWords)

	words := strings.Split(slug, "-")
	if len(words) > 5 {
		t.Errorf("GenerateSlug produced more than 5 words: %d, slug=%q", len(words), slug)
	}
}

func TestGenerateSlug_Idempotent(t *testing.T) {
	// Running slug generation twice should produce same result
	input := "Add dark mode toggle"
	first := GenerateSlug(input)
	second := GenerateSlug(first)

	if first != second {
		t.Errorf("GenerateSlug is not idempotent: first=%q, second=%q", first, second)
	}
}

func TestGenerateSlug_NoConsecutiveHyphens(t *testing.T) {
	inputs := []string{
		"Fix   bug",
		"Fix - - bug",
		"Fix---bug",
		"Fix - bug - fix",
		"  Fix  --  bug  ",
	}

	for _, input := range inputs {
		slug := GenerateSlug(input)
		if strings.Contains(slug, "--") {
			t.Errorf("GenerateSlug(%q) produced consecutive hyphens: %q", input, slug)
		}
	}
}

func TestGenerateSlug_StartsAndEndsWithAlphanumeric(t *testing.T) {
	inputs := []string{
		"---Fix bug---",
		"   Fix bug   ",
		"---Fix bug   ",
		"   Fix bug---",
		"-a-",
	}

	for _, input := range inputs {
		slug := GenerateSlug(input)
		if slug == "" {
			continue // Empty slugs are allowed
		}
		if slug[0] == '-' {
			t.Errorf("GenerateSlug(%q) starts with hyphen: %q", input, slug)
		}
		if slug[len(slug)-1] == '-' {
			t.Errorf("GenerateSlug(%q) ends with hyphen: %q", input, slug)
		}
	}
}
