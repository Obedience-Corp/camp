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
	for name, text := range map[string]string{
		"no fence":       "# Title\n\nBody.",
		"fence not shut": "---\ntitle: open\n\nBody.",
		"longer rule":    "----\ntitle: rule\n----\nBody.",
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
