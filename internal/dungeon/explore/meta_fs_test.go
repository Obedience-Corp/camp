//go:build container_fs

package explore

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const goalFixture = `---
fest_id: FA0042
fest_name: goal-named-festival
---

# Festival Goal

**Primary Goal:** Ship the goal-file fallback.
`

func TestBuildFestivalGoalFillsInForUnusableFestYAML(t *testing.T) {
	for name, festYAML := range map[string]string{
		"malformed":           "metadata:\n  id: [FA0001\n  name: wrong\n",
		"over the read limit": "metadata:\n  name: \"" + strings.Repeat("a", metaReadLimit) + "\"\n  id: FA0001\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			const rel = "festivals/.dungeon/completed/2026-10-05/broken-festival"
			dir := filepath.Join(root, filepath.FromSlash(rel))
			mustWrite(t, filepath.Join(dir, "fest.yaml"), festYAML)
			mustWrite(t, filepath.Join(dir, "FESTIVAL_GOAL.md"), goalFixture)

			idx, err := Build(context.Background(), root)
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			item := itemAt(t, idx, rel)
			if item.Kind != KindFestival || item.ID != "FA0042" || item.Title != "goal-named-festival" || item.Summary != "Ship the goal-file fallback." {
				t.Fatalf("row = %+v, want id, title, and summary from FESTIVAL_GOAL.md", item)
			}
			if !strings.HasPrefix(item.Warning, "fest.yaml: ") {
				t.Fatalf("warning = %q, want the fest.yaml failure kept", item.Warning)
			}
			if strings.Contains(item.Warning, root) {
				t.Fatalf("warning carries an absolute path: %q", item.Warning)
			}
		})
	}
}

func TestBuildWorkitemReadmeFillsInForMalformedMarker(t *testing.T) {
	root := t.TempDir()
	const rel = "workflow/design/.dungeon/completed/broken-marker"
	dir := filepath.Join(root, filepath.FromSlash(rel))
	mustWrite(t, filepath.Join(dir, ".workitem"), "title: [unclosed\nref: WI-abc\n")
	mustWrite(t, filepath.Join(dir, "README.md"), "# Broken marker\n\nThe README still says what this was.\n")

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	item := itemAt(t, idx, rel)
	if item.Kind != KindWorkitem || item.Title != "broken-marker" || item.Summary != "The README still says what this was." {
		t.Fatalf("row = %+v, want the README summary under the directory name", item)
	}
	if !strings.HasPrefix(item.Warning, ".workitem: ") || strings.Contains(item.Warning, root) {
		t.Fatalf("warning = %q, want the marker failure without an absolute path", item.Warning)
	}
}

func TestBuildMarkdownFrontmatterAsWritten(t *testing.T) {
	root := t.TempDir()
	done := filepath.Join(root, ".campaign", "intents", ".dungeon", "done")
	const relDir = ".campaign/intents/.dungeon/done/"
	mustWrite(t, filepath.Join(done, "typed.md"), "---\ntitle: 2026-10-01\nid: 0012\n---\n\nTyped scalars.\n")
	mustWrite(t, filepath.Join(done, "bare.md"), "---\ntitle:\nid: 1e3\n---\n\nA key with no value.\n")
	mustWrite(t, filepath.Join(done, "bom.md"), "\U0000FEFF---\ntitle: Marked\n---\n\nStarts with a byte order mark.\n")
	mustWrite(t, filepath.Join(done, "spaced.md"), "--- \ntitle: Spaced\n--- \n\nFences with a trailing space.\n")
	mustWrite(t, filepath.Join(done, "dated.md"), "---\ntitle: Dated\nstatus: dungeon/done\nupdated_at: 2026-10-05\n---\n\nA date with no time.\n")
	mustWrite(t, filepath.Join(done, "broken.md"), "---\ntitle: [unclosed\nstatus: dungeon/done\n---\n\nThe body is the summary.\n")
	if err := unix.Mkfifo(filepath.Join(done, "pending.md"), 0o600); err != nil {
		t.Fatal(err)
	}

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := itemAt(t, idx, relDir+"typed.md"); got.Title != "2026-10-01" || got.ID != "0012" || got.Kind != KindMarkdown {
		t.Fatalf("typed scalars were rewritten: %+v", got)
	}
	if got := itemAt(t, idx, relDir+"bare.md"); got.Title != "bare.md" || got.ID != "1e3" || got.Kind != KindMarkdown {
		t.Fatalf("empty title or exponent id was rewritten: %+v", got)
	}
	for file, title := range map[string]string{"bom.md": "Marked", "spaced.md": "Spaced"} {
		if got := itemAt(t, idx, relDir+file); got.Title != title || got.Kind != KindMarkdown || got.Warning != "" || strings.Contains(got.Summary, "title") {
			t.Fatalf("%s frontmatter was not read: %+v", file, got)
		}
	}
	if got := itemAt(t, idx, relDir+"dated.md"); got.DoneDate != "2026-10-05" || got.DateSource != DateHistory {
		t.Fatalf("date-only updated_at was dropped: %+v", got)
	}
	broken := itemAt(t, idx, relDir+"broken.md")
	if broken.Kind != KindMarkdown || broken.Summary != "The body is the summary." {
		t.Fatalf("unparsed frontmatter leaked into the row: %+v", broken)
	}
	if !strings.HasPrefix(broken.Warning, "broken.md: frontmatter: ") {
		t.Fatalf("warning = %q, want the frontmatter failure", broken.Warning)
	}
	if got := itemAt(t, idx, relDir+"pending.md"); got.Warning != "pending.md: not a regular file" {
		t.Fatalf("special file warning = %q", got.Warning)
	}
}

func TestBuildReportsAnUnreadableDirectoryOnce(t *testing.T) {
	root := t.TempDir()
	const completed = "festivals/.dungeon/completed/note.md"
	mustWrite(t, filepath.Join(root, filepath.FromSlash(completed)), "# Note\n\nShipped the note.\n")
	mustWrite(t, filepath.Join(root, "festivals", ".dungeon", "archived", "old.md"), "# Old\n\nArchived note.\n")

	// Discovery and the fingerprint walk both list the tree through walkDir,
	// so one denied directory reaches Build twice, as the OS would report it.
	orig := walkDir
	walkDir = func(walkRoot string, fn fs.WalkDirFunc) error {
		return orig(walkRoot, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry != nil && entry.IsDir() && entry.Name() == "archived" {
				if callErr := fn(path, entry, nil); callErr != nil {
					return callErr
				}
				if callErr := fn(path, entry, &fs.PathError{Op: "open", Path: path, Err: syscall.EACCES}); callErr != nil {
					return callErr
				}
				return fs.SkipDir
			}
			return fn(path, entry, err)
		})
	}
	t.Cleanup(func() { walkDir = orig })

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if itemAt(t, idx, completed).Status != "completed" {
		t.Fatal("readable item was dropped")
	}
	if len(idx.Warnings) != 1 || idx.Warnings[0] != "festivals/.dungeon/archived: permission denied" {
		t.Fatalf("warnings = %#v, want the directory named once by its camp-relative path", idx.Warnings)
	}
}
