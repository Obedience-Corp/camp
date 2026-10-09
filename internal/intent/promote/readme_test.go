package promote

import (
	"strings"
	"testing"
)

const (
	testTitle = "Effort levels"
	testID    = "effort-levels-20261008-200730"
	testDate  = "2026-10-08"
)

func TestComposeDesignReadme_TemplateIntentWritesBodyOnce(t *testing.T) {
	content := "# Effort levels\n\n## Description\n\nAdd effort levels to the agent.\n\n" +
		"## Context\n\n<!-- Why is this needed? What triggered this idea? -->\n\n" +
		"## Notes\n\n<!-- Additional thoughts, references, or considerations -->\n"

	got := composeDesignReadme(testTitle, testID, testDate, content)

	want := "# Effort levels\n\n## Content\n\n## Description\n\nAdd effort levels to the agent.\n\n" +
		"## Status\n\nIn progress \u2014 promoted from intent effort-levels-20261008-200730 on 2026-10-08.\n"
	if got != want {
		t.Fatalf("README mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestComposeDesignReadme_KeepsRemainingBodyAndNonEmptySections(t *testing.T) {
	content := "# Keep\n\n## Description\n\nFirst paragraph.\n\nSecond paragraph.\n\n" +
		"## Context\n\n<!-- Why is this needed? What triggered this idea? -->\nReal context.\n\n## Notes\n\n<!-- Additional thoughts, references, or considerations -->\n"

	got := composeDesignReadme("Keep", "keep-20261008-200731", testDate, content)

	for _, w := range []string{"## Description", "Second paragraph.", "## Description\n\nFirst paragraph.", "Real context."} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
	if strings.Count(got, "First paragraph.") != 1 {
		t.Errorf("first paragraph duplicated:\n%s", got)
	}
	if strings.Count(got, "# Keep") != 1 {
		t.Errorf("title duplicated:\n%s", got)
	}
	if strings.Contains(got, "## Notes") || strings.Contains(got, "<!--") {
		t.Errorf("empty placeholder kept:\n%s", got)
	}
}

func TestComposeDesignReadme_EmptyBody(t *testing.T) {
	got := composeDesignReadme("Empty body", "empty-20261008-200732", testDate, "")
	want := "# Empty body\n\n## Status\n\nIn progress \u2014 promoted from intent empty-20261008-200732 on 2026-10-08.\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestComposeDesignReadme_FencedHeadingsAreNotSplit(t *testing.T) {
	content := "# T\n\n## Description\n\nIntro.\n\n```md\n## Notes\n```\n"
	got := composeDesignReadme("T", "t-20261008-200733", testDate, content)
	if !strings.Contains(got, "```md\n## Notes\n```") {
		t.Errorf("fenced block altered:\n%s", got)
	}
}

func TestComposeDesignReadme_PreservesAuthoredMarkdown(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"user comments", "## Context\n\n<!-- Keep this rationale. -->\n\n## Notes\n\n<!-- TODO: confirm scope. -->"},
		{"literal comments", "## Notes\n\n```html\n<!-- Additional thoughts, references, or considerations -->\n<!-- example -->\n```"},
		{"tilde fence", "Intro.\n\n~~~md\n## Notes\n## Example\n~~~"},
		{"long backtick fence", "Intro.\n\n````md\n```\n## Notes\n## Example\n```\n````"},
		{"mismatched fence character", "Intro.\n\n~~~md\n```\n## Notes\n## Example\n~~~"},
		{"closing fence with info", "Intro.\n\n```md\n```still code\n## Notes\n## Example\n```"},
		{"longer closing fence", "Intro.\n\n~~~md\n## Notes\n## Example\n~~~~"},
		{"indented fence", "Intro.\n\n   ~~~md\n## Notes\n## Example\n   ~~~"},
		{"summary substring in heading", "## Description\n\n### Retry request\n\nRetry"},
		{"code after opening paragraph", "Intro.\n\n    first()\n    second()"},
		{"code at section boundary", "## Example\n\n    first()\n    second()"},
		{"indented literal placeholder", "## Notes\n\n    <!-- Additional thoughts, references, or considerations -->"},
		{"tab-indented literal placeholder", "## Notes\n\n\t<!-- Additional thoughts, references, or considerations -->"},
		{"mixed-indent literal placeholder", "## Notes\n\n \t<!-- Additional thoughts, references, or considerations -->"},
		{"meaningful section label", "## Non-goals\n\nSupport offline mode."},
		{"nested section label", "## Description\n\n### Non-goals\n\nSupport offline mode."},
		{"hard line break", "First line.  \nSecond line.  "},
		{"leading code", "    first()\n    second()"},
		{"empty custom heading", "## Decisions"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := composeDesignReadme(testTitle, testID, testDate, "# "+testTitle+"\n\n"+tt.body+"\n\n")
			want := "# " + testTitle + "\n\n## Content\n\n" + tt.body + "\n\n## Status\n\n" +
				"In progress — promoted from intent " + testID + " on " + testDate + ".\n"
			if got != want {
				t.Fatalf("authored Markdown changed:\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func TestComposeDesignReadme_RemovesPlaceholdersAfterClosingFence(t *testing.T) {
	for _, fence := range []string{"```", "~~~~", "   ~~~"} {
		t.Run(fence, func(t *testing.T) {
			body := "Intro.\n\n" + fence + "md\n## Notes\n" + fence
			content := body + "\n\n## Notes\n\n<!-- Additional thoughts, references, or considerations -->"
			got := composeDesignReadme(testTitle, testID, testDate, content)
			if !strings.Contains(got, body) || strings.Count(got, "## Notes") != 1 || strings.Contains(got, "<!--") {
				t.Fatalf("fence boundary or placeholder cleanup changed content:\n%s", got)
			}
		})
	}
}

func TestComposeDesignReadme_OnlyRemovesMatchingOpeningTitle(t *testing.T) {
	cases := []struct {
		name    string
		content string
		body    string
	}{
		{"matching title", "# Effort levels\n\nIntro.", "Intro."},
		{"matching title after blank lines", "\n \n# Effort levels\n\nIntro.", "Intro."},
		{"matching title with CRLF", "# Effort levels\r\n\r\nIntro.", "Intro."},
		{"custom heading", "# Migration plan\n\nIntro.", "# Migration plan\n\nIntro."},
		{"title prefix", "# Effort levels migration\n\nIntro.", "# Effort levels migration\n\nIntro."},
		{"different case", "# Effort Levels\n\nIntro.", "# Effort Levels\n\nIntro."},
		{"matching title later in body", "Intro.\n\n# Effort levels\n\nDetails.", "Intro.\n\n# Effort levels\n\nDetails."},
		{"custom heading after matching title", "# Effort levels\n\n# Migration plan\n\nIntro.", "# Migration plan\n\nIntro."},
		{"indented literal heading", "    # Effort levels\n    example", "    # Effort levels\n    example"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := composeDesignReadme(testTitle, testID, testDate, tt.content)
			want := "# " + testTitle + "\n\n## Content\n\n" + tt.body + "\n\n## Status\n\n" +
				"In progress — promoted from intent " + testID + " on " + testDate + ".\n"
			if got != want {
				t.Fatalf("opening heading changed incorrectly:\n got: %q\nwant: %q", got, want)
			}
		})
	}
}
