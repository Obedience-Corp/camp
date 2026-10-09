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

	want := "# Effort levels\n\n## Context\n\nAdd effort levels to the agent.\n\n" +
		"## Status\n\nIn progress \u2014 promoted from intent effort-levels-20261008-200730 on 2026-10-08.\n"
	if got != want {
		t.Fatalf("README mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestComposeDesignReadme_KeepsRemainingBodyAndNonEmptySections(t *testing.T) {
	content := "# Keep\n\n## Description\n\nFirst paragraph.\n\nSecond paragraph.\n\n" +
		"## Context\n\n<!-- hint -->\nReal context.\n\n## Notes\n\n<!-- hint -->\n"

	got := composeDesignReadme("Keep", "keep-20261008-200731", testDate, content)

	for _, w := range []string{"## Description", "Second paragraph.", "## Context\n\nFirst paragraph.", "Real context."} {
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
