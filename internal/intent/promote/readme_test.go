package promote

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/renderer/html"
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

func TestComposeDesignReadme_PlaceholderBlockBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		before string
		after  string
	}{
		{"indented code after paragraph", "Example:\n", "    <token>\n"},
		{"separate paragraphs", "First paragraph.\n", "Second paragraph.\n"},
		{"thematic break", "Paragraph.\n", "---\n"},
		{"ordered list after paragraph", "Steps:\n", "2. Second step\n"},
		{"separate ordered lists", "1. First list\n", "1. Restart numbering\n"},
		{"separate unordered lists", "- First list\n", "- Second list\n"},
		{"separate quotes", "> First quote\n", "> Second quote\n"},
		{"separate code blocks", "    first()\n", "    second()\n"},
		{"code after list", "- List item\n", "    <token>\n"},
		{"placeholder at section start", "", "    <token>\n"},
		{"placeholder at section end", "Paragraph.\n", ""},
	}
	md := goldmark.New(goldmark.WithRendererOptions(html.WithUnsafe()))
	for heading, hint := range map[string]string{
		"## Context": "<!-- Why is this needed? What triggered this idea? -->",
		"## Notes":   "<!-- Additional thoughts, references, or considerations -->",
	} {
		for name, newline := range map[string]string{"LF": "\n", "CRLF": "\r\n"} {
			for _, tt := range cases {
				t.Run(heading+"/"+tt.name+"/"+name, func(t *testing.T) {
					body := strings.ReplaceAll(heading+"\n\n"+tt.before+hint+"\n"+tt.after, "\n", newline)
					cleaned := cleanDesignBody(testTitle, body)
					if strings.Contains(cleaned, hint) {
						t.Fatalf("template text was retained: %q", cleaned)
					}
					var originalHTML, cleanedHTML bytes.Buffer
					if err := md.Convert([]byte(body), &originalHTML); err != nil {
						t.Fatal(err)
					}
					if err := md.Convert([]byte(cleaned), &cleanedHTML); err != nil {
						t.Fatal(err)
					}
					// Compare the rendered authored blocks, excluding only the
					// invisible template/separator comments. This catches changed
					// code, list numbering, quote boundaries and paragraph breaks.
					want := strings.ReplaceAll(originalHTML.String(), hint+newline, "")
					got := strings.ReplaceAll(cleanedHTML.String(), "<!---->"+newline, "")
					if got != want {
						t.Fatalf("authored block structure changed:\n got: %s\nwant: %s\nMarkdown: %q", got, want, cleaned)
					}
					assertDesignStatusRendered(t, composeDesignReadme(testTitle, testID, testDate, body))
				})
			}
		}
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
		{"nonbreaking space in notes", "## Notes\n\n\u00a0\n\n## Details\n\nText."},
		{"leading nonbreaking space", "\u00a0\n\nIntro."},
		{"authored spacing", "Intro.\n\n\n## Details\nText.\n\n\n## More\nMore text."},
		{"reference definition in notes", "## Notes\n\n[design]: https://example.com/design"},
		{"placeholder in subsection", "## Notes\n\n### Example\n\n<!-- Additional thoughts, references, or considerations -->"},
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

func TestComposeDesignReadme_PreservesLiteralBlocks(t *testing.T) {
	cases := []string{
		"<pre>\n## Notes\n## Example\n</pre>",
		"<PRE class=example>\n## Notes\n## Example\n</PRE>",
		"<script>\n## Notes\n## Example\n</script>",
		"<style>\n## Notes\n## Example\n</style>",
		"<textarea>\n## Notes\n## Example\n</textarea>",
		"<!--\n## Notes\n## Example\n-->",
		"<?example\n## Notes\n## Example\n?>",
		"<!DOCTYPE\n## Notes\n## Example\n>",
		"<![CDATA[\n## Notes\n## Example\n]]>",
		"<div>\n## Notes\n## Example\n</div>",
		"<custom-tag>\n## Notes\n## Example\n</custom-tag>",
		"<pre>\n```\n## Notes\n</pre>",
		"```html\n<pre>\n## Notes\n```",
		"<pre>\n<!-- Additional thoughts, references, or considerations -->\n</pre>",
		"<!-- A user note\n<!-- Additional thoughts, references, or considerations -->",
		"> ## Notes\n>\n> <!-- Additional thoughts, references, or considerations -->",
		"- Example:\n\n  ~~~md\n  ## Notes\n  ## Example\n  ~~~",
		"- Notes:\n  <!-- Additional thoughts, references, or considerations -->",
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			content := "# " + testTitle + "\n\n## Notes\n\n" + body +
				"\n\n## Context\n\n<!-- Why is this needed? What triggered this idea? -->\n"
			got := composeDesignReadme(testTitle, testID, testDate, content)
			wantBody := "## Notes\n\n" + body
			if !strings.Contains(got, wantBody) {
				t.Fatalf("literal block changed:\nwant body: %q\n got: %q", wantBody, got)
			}
			if strings.Contains(got, "## Context") {
				t.Fatalf("real template placeholder after literal block was retained:\n%s", got)
			}
			assertDesignStatusRendered(t, got)
		})
	}
}

func TestComposeDesignReadme_StatusOutsideEOFBlocks(t *testing.T) {
	cases := []string{
		"```",
		"```go\nfirst()",
		"````md\n```\n## Notes\n## Example",
		"~~~md\n## Notes\n## Example",
		"   ~~~md\n## Notes\n## Example",
		"```go\nfirst()\n\n\n",
		"```go\r\nfirst()\r\n",
		"> ```md\n> ## Notes",
		"- ```md\n  ## Notes",
		"<pre>\n## Notes\n## Example",
		"<SCRIPT>\n## Notes\n## Example",
		"<pre\fclass=example>\n## Notes\n## Example",
		"<style>\n## Notes\n## Example",
		"<textarea>\n## Notes\n## Example",
		"<!--\n## Notes\n## Example",
		"<?example\n## Notes\n## Example",
		"<!DOCTYPE\n## Notes\n## Example",
		"<![CDATA[\n## Notes\n## Example",
		"<div>\n## Notes\n## Example",
		"<custom-tag>\n## Notes\n## Example",
		"<pre></pre>",
		"<!-- closed -->",
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			got := composeDesignReadme(testTitle, testID, testDate, body)
			if !strings.Contains(got, body) {
				t.Fatalf("EOF block content changed:\nwant body: %q\n got: %q", body, got)
			}
			assertDesignStatusRendered(t, got)
		})
	}
}

func assertDesignStatusRendered(t *testing.T, readme string) {
	t.Helper()
	var rendered bytes.Buffer
	if err := goldmark.Convert([]byte(readme), &rendered); err != nil {
		t.Fatal(err)
	}
	want := "<h2>Status</h2>\n<p>In progress — promoted from intent " + testID + " on " + testDate + ".</p>"
	if !strings.HasSuffix(strings.TrimSpace(rendered.String()), want) {
		t.Fatalf("status/provenance did not render outside authored blocks:\n%s", rendered.String())
	}
}

func FuzzComposeDesignReadme_StatusRendered(f *testing.F) {
	for _, body := range []string{
		"", "Intro.", "```", "~~~md\n## Notes", "<pre>\n## Notes", "<!--", "<?xml", "<![CDATA[",
		"## Notes\n\n<!-- Additional thoughts, references, or considerations -->",
		"## Context\n\n## Notes\n\n# Effort levels", "> ```\n> ## Notes", "- ```\n  ## Notes",
	} {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, body string) {
		assertDesignStatusRendered(t, composeDesignReadme(testTitle, testID, testDate, body))
	})
}
