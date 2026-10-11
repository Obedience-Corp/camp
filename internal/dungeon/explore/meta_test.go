package explore

import (
	"io/fs"
	"maps"
	"strings"
	"syscall"
	"testing"
)

func TestFrontmatterKeepsScalarsAsWritten(t *testing.T) {
	text := strings.Join([]string{
		"---",
		"title: 2026-10-01",
		"id: 0012",
		"ref: 1e3",
		"flag: true",
		"empty:",
		"none: null",
		"tilde: ~",
		`quoted: "null"`,
		"tags: [a, b]",
		"---",
		"",
		"Body line.",
	}, "\n")
	fm, body, err := splitFrontmatter(text)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"title": "2026-10-01", "id": "0012", "ref": "1e3", "flag": "true",
		"empty": "", "none": "", "tilde": "", "quoted": "null", "tags": "",
	}
	if !maps.Equal(fm, want) {
		t.Fatalf("frontmatter = %#v\nwant %#v", fm, want)
	}
	if body != "\nBody line." {
		t.Fatalf("body = %q", body)
	}
}

func TestFrontmatterAcceptsByteOrderMarkAndPaddedFences(t *testing.T) {
	for name, text := range map[string]string{
		"byte order mark": "\U0000FEFF---\ntitle: Kept\n---\nBody.",
		"trailing space":  "--- \ntitle: Kept\n---  \nBody.",
		"trailing tab":    "---\t\ntitle: Kept\n---\t\nBody.",
		"carriage return": "---\r\ntitle: Kept\r\n---\r\nBody.",
		"all of them":     "\U0000FEFF--- \t\r\ntitle: Kept\r\n--- \r\nBody.",
	} {
		t.Run(name, func(t *testing.T) {
			fm, body, err := splitFrontmatter(text)
			if err != nil {
				t.Fatal(err)
			}
			if fm["title"] != "Kept" || body != "Body." {
				t.Fatalf("frontmatter = %#v, body = %q", fm, body)
			}
			if got := MarkdownBody(text); got != "Body." {
				t.Fatalf("MarkdownBody = %q", got)
			}
		})
	}
}

func TestFrontmatterReportsBlockThatDoesNotParse(t *testing.T) {
	for name, block := range map[string]string{
		"invalid YAML":  "title: [unclosed\nstatus: dungeon/done",
		"not a mapping": "title without a colon",
	} {
		t.Run(name, func(t *testing.T) {
			text := "---\n" + block + "\n---\nBody line."
			fm, body, err := splitFrontmatter(text)
			if err == nil {
				t.Fatalf("no error; frontmatter = %#v", fm)
			}
			if fm != nil || body != "Body line." {
				t.Fatalf("frontmatter = %#v, body = %q; the raw block must not reach the body", fm, body)
			}
			if got := MarkdownBody(text); got != text {
				t.Fatalf("MarkdownBody = %q, want the document left whole for the reader", got)
			}
		})
	}
	// Without a closing fence nothing says where the block ends, so none of
	// the document may be taken for its body.
	for name, text := range map[string]string{
		"fence not shut":           "---\ntitle: open\napi_key: hunter2\n\nBody.",
		"closed with a YAML end":   "---\ntitle: open\n...\nBody.",
		"cut inside the block":     "---\ntitle: open\nnotes: |\n  " + strings.Repeat("a", 64),
		"second block is not shut": "---\ntitle: open",
	} {
		t.Run(name, func(t *testing.T) {
			fm, body, err := splitFrontmatter(text)
			if err == nil {
				t.Fatalf("no error; frontmatter = %#v, body = %q", fm, body)
			}
			if fm != nil || body != "" {
				t.Fatalf("frontmatter = %#v, body = %q; an open block must not reach the body", fm, body)
			}
			if got := MarkdownBody(text); got != text {
				t.Fatalf("MarkdownBody = %q, want the document left whole for the reader", got)
			}
		})
	}
	for name, text := range map[string]string{
		"no fence":    "# Title\n\nBody.",
		"longer rule": "----\ntitle: rule\n----\nBody.",
		"fence alone": "---",
	} {
		t.Run(name, func(t *testing.T) {
			fm, body, err := splitFrontmatter(text)
			if err != nil || fm != nil || body != text {
				t.Fatalf("frontmatter = %#v, body = %q, err = %v; want the text untouched", fm, body, err)
			}
		})
	}
	if fm, body, err := splitFrontmatter("---\n---\nBody."); err != nil || fm == nil || len(fm) != 0 || body != "Body." {
		t.Fatalf("empty block: frontmatter = %#v, body = %q, err = %v", fm, body, err)
	}
}

func TestParseStampAcceptsYAMLTimestamps(t *testing.T) {
	for value, want := range map[string]string{
		"2026-10-10T16:50:31.883621-07:00": "2026-10-10",
		"2026-10-05T12:00:00Z":             "2026-10-05",
		"2026-10-05 12:00:00":              "2026-10-05",
		"2026-10-05":                       "2026-10-05",
		"2026-1-5":                         "2026-01-05",
	} {
		if got, ok := parseStamp(value); !ok || got != want {
			t.Errorf("parseStamp(%q) = %q, %v; want %q", value, got, ok, want)
		}
	}
	for _, value := range []string{"", "yesterday", "2026-13-01"} {
		if got, ok := parseStamp(value); ok {
			t.Errorf("parseStamp(%q) = %q, want no date", value, got)
		}
	}
}

func TestWarningsNameCampRelativePathsOnce(t *testing.T) {
	const root = "/camp"
	denied := &fs.PathError{Op: "open", Path: root + "/festivals/.dungeon/archived", Err: syscall.EACCES}
	for name, got := range map[string]string{
		"path below the label":  warn(root, "festivals/.dungeon", denied),
		"path equals the label": warn(root, "festivals/.dungeon/archived", denied),
	} {
		if got != "festivals/.dungeon/archived: permission denied" {
			t.Errorf("%s: warn = %q", name, got)
		}
	}
	outside := &fs.PathError{Op: "open", Path: "/elsewhere/secret", Err: syscall.EACCES}
	if got := warn(root, "festivals/.dungeon", outside); got != "festivals/.dungeon: permission denied" {
		t.Errorf("path outside the camp: warn = %q", got)
	}
	if got := warn(root, "festivals/.dungeon", syscall.EIO); got != "festivals/.dungeon: input/output error" {
		t.Errorf("plain error: warn = %q", got)
	}
	if got := readWarning(root+"/festivals/.dungeon/done/x/fest.yaml", denied); got != "fest.yaml: permission denied" {
		t.Errorf("readWarning = %q", got)
	}

	discovered := "festivals/.dungeon/archived: permission denied"
	got := addWarnings(nil, discovered)
	got = addWarnings(got, warn(root, "festivals/.dungeon", denied), warn(root, "festivals/.dungeon/archived", denied), "")
	if len(got) != 1 || got[0] != discovered {
		t.Fatalf("warnings = %#v, want the directory reported once", got)
	}
}

func TestFirstParagraphSkipsLeadingComments(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"template prompt":         {"# Title\n\n<!-- Why is this needed? -->\n\nBecause it broke.\n", "Because it broke."},
		"comment over many lines": {"<!--\nWhy is this needed?\nWhat triggered it?\n-->\n\nBecause it broke.\n", "Because it broke."},
		"two comments":            {"<!-- one -->\n<!-- two -->\nBecause it broke.\n", "Because it broke."},
		"text after the comment":  {"<!-- note --> Because it broke.\nSecond line.\n", "Because it broke. Second line."},
		"comment never closed":    {"# Title\n\n<!-- Why is this needed?\n\nBecause it broke.\n", ""},
		"only a comment":          {"<!-- Why is this needed? -->\n", ""},
		"comment inside the text": {"Because <!-- aside --> it broke.\n", "Because <!-- aside --> it broke."},
		"no comment":              {"# Title\n\nBecause it broke.\nSecond line.\n\nNext.\n", "Because it broke. Second line."},
	} {
		if got := firstParagraph(tc.body); got != tc.want {
			t.Errorf("%s: firstParagraph = %q, want %q", name, got, tc.want)
		}
	}
	const goal = "<!-- fill this in -->\n\n**Primary Goal:** Ship it.\n"
	if got := primaryGoal(goal); got != "Ship it." {
		t.Errorf("primaryGoal = %q", got)
	}
}

// A date bucket is named for the mover's calendar day. A recorded move reads
// as the same day when it keeps the offset it was written with, whatever zone
// the feed is opened in.
func TestHistoryDayIsTheDayInTheStampsOwnOffset(t *testing.T) {
	for value, want := range map[string]string{
		"2026-10-05T23:30:00-06:00": "2026-10-05",
		"2026-10-05T00:30:00+09:00": "2026-10-05",
		"2026-10-06T05:30:00Z":      "2026-10-06",
	} {
		if got, ok := parseStamp(value); !ok || got != want {
			t.Errorf("parseStamp(%q) = %q, %v; want %q", value, got, ok, want)
		}
		got, ok := newestDungeonDay([]historyEntry{
			{Status: "active", Timestamp: "2030-01-01T00:00:00Z"},
			{Status: "dungeon/completed", Timestamp: value},
		})
		if !ok || got != want {
			t.Errorf("newestDungeonDay(%q) = %q, %v; want %q", value, got, ok, want)
		}
	}
}

func TestPrimaryGoalReadsTheLineUnderItsLabel(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"same line":          {"# Goal\n\n**Primary Goal:** Ship it.\n\n**Vision:** More.\n", "Ship it."},
		"label among others": {"**Festival:** x | **Primary Goal:** Ship it.\n", "Ship it."},
		"next line":          {"## Objective\n\n**Primary Goal:**\nShip it\nacross two lines.\n\n**Vision:** More.\n", "Ship it across two lines."},
		"after a blank line": {"**Primary Goal:**\n\nShip it.\n\nNext paragraph.\n", "Ship it."},
		"after a prompt":     {"**Primary Goal:**\n<!-- one sentence -->\nShip it.\n", "Ship it."},
		"next label instead": {"**Primary Goal:**\n**Vision:** More.\n", ""},
		"heading instead":    {"**Primary Goal:**\n\n## Success Criteria\n\nIt works.\n", ""},
		"nothing after":      {"**Primary Goal:**", ""},
		"no label":           {"# Goal\n\nShip it.\n", "Ship it."},
	} {
		if got := primaryGoal(tc.body); got != tc.want {
			t.Errorf("%s: primaryGoal = %q, want %q", name, got, tc.want)
		}
	}
}

// fest leaves these in a template for the author. Text that still opens with
// one says nothing about the work.
func TestTidyDropsATemplatePlaceholderSummary(t *testing.T) {
	for _, summary := range []string{
		"[REPLACE: One sentence describing what this festival accomplishes]",
		"[FILL: the outcome]",
		"[GUIDANCE: say why]  and more",
		"{{ .Goal }}",
		"\x1b[1m[REPLACE: hidden behind an escape]",
	} {
		item := Item{Title: "t", Summary: summary}
		tidy(&item, "t")
		if item.Summary != "" {
			t.Errorf("summary %q was kept as %q", summary, item.Summary)
		}
	}
	for _, summary := range []string{"Replace the old cache.", "[x] done", "See [REPLACE: later] for more.", "{{not a marker"} {
		item := Item{Title: "t", Summary: summary}
		tidy(&item, "t")
		if item.Summary != summary {
			t.Errorf("summary %q became %q", summary, item.Summary)
		}
	}
}
