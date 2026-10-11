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
	mustWrite(t, filepath.Join(done, "open.md"), "---\ntitle: Secret Title\napi_key: hunter2\n\nNo closing fence.\n")
	mustWrite(t, filepath.Join(done, "ended.md"), "---\ntitle: Ended\napi_key: hunter2\n...\nClosed the YAML way.\n")
	mustWrite(t, filepath.Join(done, "long.md"), "---\ntitle: Long\nnotes: "+strings.Repeat("a", metaReadLimit)+"\n---\n\nPast the read limit.\n")
	mustWrite(t, filepath.Join(done, "blank.md"), "---\ntitle: \"\\u200B\\u202E\"\n---\n\nA title with nothing to show.\n")
	mustWrite(t, filepath.Join(done, "prompted.md"), "---\ntitle: Prompted\n---\n\n# Prompted\n\n<!-- Why is this needed?\n     What triggered it? -->\n\nIt broke on a pipe.\n")
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
	for _, file := range []string{"open.md", "ended.md", "long.md"} {
		got := itemAt(t, idx, relDir+file)
		if got.Kind != KindMarkdown || got.Title != file || got.Summary != "" {
			t.Fatalf("%s: a block with no closing fence reached the row: %+v", file, got)
		}
		if got.Warning != file+": frontmatter has no closing --- line" {
			t.Fatalf("%s: warning = %q", file, got.Warning)
		}
	}
	if got := itemAt(t, idx, relDir+"blank.md"); got.Title != "blank.md" || got.Summary != "A title with nothing to show." {
		t.Fatalf("blank title did not fall back to the file name: %+v", got)
	}
	if got := itemAt(t, idx, relDir+"prompted.md"); got.Summary != "It broke on a pipe." {
		t.Fatalf("template comment became the summary: %q", got.Summary)
	}
}

// yaml.v3 fills every field it can before it reports one of the wrong kind.
func TestBuildKeepsFieldsBesideAWronglyTypedOne(t *testing.T) {
	root := t.TempDir()
	const festRel = "festivals/.dungeon/completed/typed-festival-TE0001"
	fest := filepath.Join(root, filepath.FromSlash(festRel))
	mustWrite(t, filepath.Join(fest, "fest.yaml"), `metadata:
  id: TE0001
  name: typed-festival
  goal:
    - a list where one line belongs
  status_history:
    - status: dungeon/completed
      timestamp: 2026-08-01T10:00:00-06:00
`)
	const workRel = "workflow/design/.dungeon/completed/typed-marker"
	work := filepath.Join(root, filepath.FromSlash(workRel))
	mustWrite(t, filepath.Join(work, ".workitem"), "id: design-typed-marker\ntitle: [a, list]\nref: WI-abc\n")
	mustWrite(t, filepath.Join(work, "README.md"), "# Typed marker\n\nThe README still says what this was.\n")

	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	festival := itemAt(t, idx, festRel)
	if festival.ID != "TE0001" || festival.Title != "typed-festival" || festival.DoneDate != "2026-08-01" || festival.DateSource != DateHistory {
		t.Fatalf("festival row lost fields that decoded: %+v", festival)
	}
	if festival.Summary != "" || !strings.HasPrefix(festival.Warning, "fest.yaml: ") || strings.Contains(festival.Warning, root) {
		t.Fatalf("festival row = %+v, want the wrong field left out and the failure kept", festival)
	}
	workitem := itemAt(t, idx, workRel)
	if workitem.Kind != KindWorkitem || workitem.ID != "WI-abc" || workitem.Title != "typed-marker" || workitem.Summary != "The README still says what this was." {
		t.Fatalf("workitem row lost fields that decoded: %+v", workitem)
	}
	if !strings.HasPrefix(workitem.Warning, ".workitem: ") {
		t.Fatalf("workitem warning = %q, want the marker failure kept", workitem.Warning)
	}
}

// fest writes the move into the goal file's frontmatter as well as into
// fest.yaml. It dates the row when fest.yaml cannot, after a date bucket and
// fest.yaml's own history.
func TestBuildFestivalGoalSuppliesMissingDate(t *testing.T) {
	const moved = "---\nfest_id: FA0042\nfest_name: goal-named-festival\nfest_status: dungeon/completed\nfest_updated: 2026-10-05T23:30:18.129491-06:00\n---\n\n**Primary Goal:** Ship it.\n"
	const broken = "metadata:\n  id: [FA0001\n"
	const noMove = "metadata:\n  id: FA0042\n  name: goal-named-festival\n  goal: Ship it.\n  status_history:\n    - status: active\n      timestamp: 2026-09-07T14:33:17-06:00\n"
	const ownMove = "metadata:\n  id: FA0042\n  name: goal-named-festival\n  goal: Ship it.\n  status_history:\n    - status: dungeon/completed\n      timestamp: 2026-09-01T09:00:00-06:00\n"
	for name, tc := range map[string]struct {
		rel, festYAML, goal string
		day, source         string
	}{
		"fest.yaml unreadable":       {"festivals/.dungeon/completed/flat", broken, moved, "2026-10-05", DateHistory},
		"fest.yaml records no move":  {"festivals/.dungeon/completed/flat", noMove, moved, "2026-10-05", DateHistory},
		"fest.yaml history wins":     {"festivals/.dungeon/completed/flat", ownMove, moved, "2026-09-01", DateHistory},
		"date bucket wins":           {"festivals/.dungeon/completed/2026-10-01/bucketed", broken, moved, "2026-10-01", DateBucket},
		"goal file not in a dungeon": {"festivals/.dungeon/completed/flat", broken, strings.Replace(moved, "dungeon/completed", "active", 1), "", DateFileTime},
		"goal file has no stamp":     {"festivals/.dungeon/completed/flat", broken, strings.Replace(moved, "fest_updated: 2026", "fest_updated: about 2026", 1), "", DateFileTime},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, filepath.FromSlash(tc.rel))
			mustWrite(t, filepath.Join(dir, "fest.yaml"), tc.festYAML)
			mustWrite(t, filepath.Join(dir, "FESTIVAL_GOAL.md"), tc.goal)
			idx, err := Build(context.Background(), root)
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			got := itemAt(t, idx, tc.rel)
			if got.DateSource != tc.source || (tc.day != "" && got.DoneDate != tc.day) {
				t.Fatalf("done %s by %s, want %s by %s: %+v", got.DoneDate, got.DateSource, tc.day, tc.source, got)
			}
			if got.ID != "FA0042" || got.Title != "goal-named-festival" {
				t.Fatalf("row = %+v", got)
			}
		})
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
