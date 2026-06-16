package mdlinks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

func TestRewriteForMove_SingleFileMoved_InternalLinksUpdated(t *testing.T) {
	root := t.TempDir()

	other := filepath.Join(root, "docs", "other.md")
	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "note.md")

	writeFile(t, other, "hello")
	writeFile(t, src, "[link](../docs/other.md)")

	if err := os.MkdirAll(filepath.Join(root, "archive"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, dst)
	if got != "[link](../docs/other.md)" {
		t.Errorf("internal link: got %q", got)
	}
}

func TestRewriteForMove_SingleFileMoved_InternalLinksRewritten(t *testing.T) {
	root := t.TempDir()

	other := filepath.Join(root, "docs", "other.md")
	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "2026-01-01", "note.md")

	writeFile(t, other, "hello")
	writeFile(t, src, "[link](../docs/other.md)")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, dst)
	want := "[link](../../docs/other.md)"
	if got != want {
		t.Errorf("internal link after deep move: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_ExternalFileLinksToMoved_Updated(t *testing.T) {
	root := t.TempDir()

	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "2026-01-01", "note.md")
	referrer := filepath.Join(root, "docs", "index.md")

	writeFile(t, src, "# Note")
	writeFile(t, referrer, "[see note](../notes/note.md)")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, referrer)
	want := "[see note](../archive/2026-01-01/note.md)"
	if got != want {
		t.Errorf("external referrer: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_ReturnsModifiedFiles(t *testing.T) {
	root := t.TempDir()

	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "note.md")
	referrer := filepath.Join(root, "docs", "index.md")

	writeFile(t, src, "# Note\n")
	writeFile(t, referrer, "[see note](../notes/note.md)\n")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	modified, err := RewriteForMove(context.Background(), root, src, dst)
	if err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	const wantExternal = "docs/index.md"
	found := false
	for _, p := range modified {
		if filepath.IsAbs(p) {
			t.Errorf("modified path %q is absolute, want campaign-relative", p)
		}
		if p == wantExternal {
			found = true
		}
	}
	if !found {
		t.Errorf("RewriteForMove returned %v, want it to include rewritten external file %q", modified, wantExternal)
	}
}

func TestRewriteForMove_ExternalURLsUntouched(t *testing.T) {
	root := t.TempDir()

	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "note.md")
	referrer := filepath.Join(root, "docs", "index.md")

	writeFile(t, src, "# Note")
	writeFile(t, referrer, "[ext](https://example.com) [local](../notes/note.md)")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, referrer)
	want := "[ext](https://example.com) [local](../archive/note.md)"
	if got != want {
		t.Errorf("mixed links: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_AbsolutePathsUntouched(t *testing.T) {
	root := t.TempDir()

	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "note.md")
	referrer := filepath.Join(root, "docs", "index.md")

	writeFile(t, src, "# Note")
	writeFile(t, referrer, "[abs](/absolute/path.md)")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, referrer)
	want := "[abs](/absolute/path.md)"
	if got != want {
		t.Errorf("absolute link: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_AnchorsOnlyUntouched(t *testing.T) {
	root := t.TempDir()

	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "note.md")
	referrer := filepath.Join(root, "docs", "index.md")

	writeFile(t, src, "# Note")
	writeFile(t, referrer, "[anchor](#section)")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, referrer)
	want := "[anchor](#section)"
	if got != want {
		t.Errorf("anchor-only link: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_DirectoryMoved_AllMDFilesRewritten(t *testing.T) {
	root := t.TempDir()

	srcDir := filepath.Join(root, "project")
	dstDir := filepath.Join(root, "archive", "project")

	writeFile(t, filepath.Join(srcDir, "README.md"), "[link](../shared/guide.md)")
	writeFile(t, filepath.Join(srcDir, "sub", "page.md"), "[link](../../shared/guide.md)")
	writeFile(t, filepath.Join(root, "shared", "guide.md"), "guide")
	referrer := filepath.Join(root, "docs", "index.md")
	writeFile(t, referrer, "[proj readme](../project/README.md)")

	if err := os.MkdirAll(filepath.Dir(dstDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(srcDir, dstDir); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, srcDir, dstDir); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	readme := readFile(t, filepath.Join(dstDir, "README.md"))
	wantReadme := "[link](../../shared/guide.md)"
	if readme != wantReadme {
		t.Errorf("moved README: got %q, want %q", readme, wantReadme)
	}

	page := readFile(t, filepath.Join(dstDir, "sub", "page.md"))
	wantPage := "[link](../../../shared/guide.md)"
	if page != wantPage {
		t.Errorf("moved sub/page.md: got %q, want %q", page, wantPage)
	}

	gotReferrer := readFile(t, referrer)
	wantReferrer := "[proj readme](../archive/project/README.md)"
	if gotReferrer != wantReferrer {
		t.Errorf("external referrer: got %q, want %q", gotReferrer, wantReferrer)
	}
}

func TestRewriteForMove_DirectoryMoved_IntraDirectoryLinksUnchanged(t *testing.T) {
	root := t.TempDir()

	srcDir := filepath.Join(root, "project")
	dstDir := filepath.Join(root, "archive", "project")

	writeFile(t, filepath.Join(srcDir, "README.md"), "[page](sub/page.md)")
	writeFile(t, filepath.Join(srcDir, "sub", "page.md"), "[back](../README.md)")
	writeFile(t, filepath.Join(root, "shared", "guide.md"), "guide")
	referrer := filepath.Join(root, "docs", "index.md")
	writeFile(t, referrer, "[proj](../project/README.md)")

	if err := os.MkdirAll(filepath.Dir(dstDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(srcDir, dstDir); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, srcDir, dstDir); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	readme := readFile(t, filepath.Join(dstDir, "README.md"))
	if readme != "[page](sub/page.md)" {
		t.Errorf("README intra-dir link: got %q, want unchanged %q", readme, "[page](sub/page.md)")
	}

	page := readFile(t, filepath.Join(dstDir, "sub", "page.md"))
	if page != "[back](../README.md)" {
		t.Errorf("sub/page.md intra-dir link: got %q, want unchanged %q", page, "[back](../README.md)")
	}

	gotReferrer := readFile(t, referrer)
	wantReferrer := "[proj](../archive/project/README.md)"
	if gotReferrer != wantReferrer {
		t.Errorf("external referrer: got %q, want %q", gotReferrer, wantReferrer)
	}
}

func TestRewriteForMove_LinkInFencedCodeBlock_Unchanged(t *testing.T) {
	root := t.TempDir()

	other := filepath.Join(root, "docs", "other.md")
	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "2026-01-01", "note.md")

	content := "```markdown\n[link](../docs/other.md)\n```\n\nOutside: [real](../docs/other.md)"
	writeFile(t, other, "hello")
	writeFile(t, src, content)

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, dst)
	want := "```markdown\n[link](../docs/other.md)\n```\n\nOutside: [real](../../docs/other.md)"
	if got != want {
		t.Errorf("fenced code block: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_LinkInInlineCode_Unchanged(t *testing.T) {
	root := t.TempDir()

	other := filepath.Join(root, "docs", "other.md")
	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "2026-01-01", "note.md")

	content := "Use `[link](../docs/other.md)` as example. Real: [link](../docs/other.md)"
	writeFile(t, other, "hello")
	writeFile(t, src, content)

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, dst)
	want := "Use `[link](../docs/other.md)` as example. Real: [link](../../docs/other.md)"
	if got != want {
		t.Errorf("inline code: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_AngleBracketDestination(t *testing.T) {
	root := t.TempDir()

	other := filepath.Join(root, "docs", "other.md")
	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "2026-01-01", "note.md")

	writeFile(t, other, "hello")
	writeFile(t, src, "[link](<../docs/other.md>)")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, dst)
	want := "[link](<../../docs/other.md>)"
	if got != want {
		t.Errorf("angle-bracket destination: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_ReferenceStyleDefinition(t *testing.T) {
	root := t.TempDir()

	other := filepath.Join(root, "docs", "other.md")
	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "2026-01-01", "note.md")

	writeFile(t, other, "hello")
	writeFile(t, src, "[link][ref]\n\n[ref]: ../docs/other.md \"title\"")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, dst)
	want := "[link][ref]\n\n[ref]: ../../docs/other.md \"title\""
	if got != want {
		t.Errorf("reference-style definition: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_ReferenceStyleDefinitionInCodeBlock_Unchanged(t *testing.T) {
	root := t.TempDir()

	other := filepath.Join(root, "docs", "other.md")
	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "2026-01-01", "note.md")

	writeFile(t, other, "hello")
	writeFile(t, src, "```\n[ref]: ../docs/other.md\n```\n\n[ref]: ../docs/other.md")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, dst)
	want := "```\n[ref]: ../docs/other.md\n```\n\n[ref]: ../../docs/other.md"
	if got != want {
		t.Errorf("ref def in code block: got %q, want %q", got, want)
	}
}

func TestRewriteForMove_RefDefInCodeBlockAfterInlineRewrites_Unchanged(t *testing.T) {
	root := t.TempDir()

	other := filepath.Join(root, "docs", "other.md")
	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "a", "b", "c", "d", "e", "note.md")

	writeFile(t, other, "hello")

	links := strings.Repeat("[x](../docs/other.md) ", 6)
	fenced := "```\n[ref]: ../docs/other.md\n```"
	writeFile(t, src, links+"\n\n"+fenced+"\n")

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := RewriteForMove(context.Background(), root, src, dst); err != nil {
		t.Fatalf("RewriteForMove: %v", err)
	}

	got := readFile(t, dst)
	if !strings.Contains(got, fenced) {
		t.Errorf("ref-def inside fenced code block was corrupted after preceding inline rewrites:\n%s", got)
	}
	if !strings.Contains(got, "../../../../../docs/other.md") {
		t.Errorf("inline links outside the fence should have been rewritten:\n%s", got)
	}
}

func TestRewriteForMove_ContextCancelled(t *testing.T) {
	root := t.TempDir()

	src := filepath.Join(root, "notes", "note.md")
	dst := filepath.Join(root, "archive", "note.md")
	writeFile(t, src, "# Note")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RewriteForMove(ctx, root, src, dst)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestIsRelative(t *testing.T) {
	cases := []struct {
		target string
		want   bool
	}{
		{"relative/path.md", true},
		{"../other.md", true},
		{"https://example.com", false},
		{"http://example.com", false},
		{"/absolute/path.md", false},
		{"#section", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isRelative(c.target); got != c.want {
			t.Errorf("isRelative(%q) = %v, want %v", c.target, got, c.want)
		}
	}
}
