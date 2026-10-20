//go:build container_fs

package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/stageguard"
)

type captureFixture struct {
	t    *testing.T
	repo string
}

func newCaptureFixture(t *testing.T) *captureFixture {
	t.Helper()
	f := &captureFixture{t: t, repo: t.TempDir()}
	f.git("init", "-q")
	f.git("config", "user.name", "Camp Test")
	f.git("config", "user.email", "camp-test@example.invalid")
	return f
}

func (f *captureFixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (f *captureFixture) write(rel, content string) {
	f.t.Helper()
	path := filepath.Join(f.repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *captureFixture) index() []byte {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.repo, ".git", "index"))
	if err != nil {
		f.t.Fatal(err)
	}
	return data
}

func (f *captureFixture) assertNoIndexBackups() {
	f.t.Helper()
	left, err := filepath.Glob(filepath.Join(f.repo, ".git", "index.camp-before-queue.*"))
	if err != nil {
		f.t.Fatal(err)
	}
	if len(left) != 0 {
		f.t.Fatalf("index backups left behind: %v", left)
	}
}

// cachedSizes reads the stat size git cached for each index entry.
func (f *captureFixture) cachedSizes() map[string]string {
	f.t.Helper()
	sizes := map[string]string{}
	current := ""
	for _, line := range strings.Split(f.git("ls-files", "--debug"), "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			current = line
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "size:" {
			sizes[current] = fields[1]
		}
	}
	return sizes
}

func (f *captureFixture) seed() {
	f.t.Helper()
	f.write("keep.md", "unchanged content\n")
	f.write("edit.md", "before\n")
	f.git("add", ".")
	f.git("commit", "-q", "-m", "seed")
	f.git("status", "--porcelain")
}

func TestInstallTreeKeepsCachedStatData(t *testing.T) {
	f := newCaptureFixture(t)
	f.seed()
	if got := f.cachedSizes()["keep.md"]; got == "0" || got == "" {
		t.Fatalf("fixture did not cache stat data: %q", got)
	}
	f.write("edit.md", "after, never staged\n")
	f.write("new.md", "new\n")

	ctx := context.Background()
	tree, err := CaptureTree(ctx, f.repo, []string{"edit.md", "new.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	finish, err := InstallTree(ctx, f.repo, tree)
	if err != nil {
		t.Fatalf("InstallTree with unstaged edits: %v", err)
	}
	if err := finish(true); err != nil {
		t.Fatal(err)
	}
	if got := f.cachedSizes()["keep.md"]; got == "0" || got == "" {
		t.Fatalf("an unchanged entry lost its cached stat data (size %q); the next status rehashes the tree", got)
	}
	if got := strings.TrimSpace(f.git("write-tree")); got != tree {
		t.Fatalf("index tree = %s, want the captured %s", got, tree)
	}
	if status := strings.TrimSpace(f.git("status", "--porcelain", "--", "edit.md", "new.md")); status != "M  edit.md\nA  new.md" {
		t.Fatalf("status = %q, want both changes staged and nothing left in the worktree", status)
	}
	f.assertNoIndexBackups()
}

func TestInstallTreeFinishFalseRestoresTheIndex(t *testing.T) {
	f := newCaptureFixture(t)
	f.seed()
	f.write("edit.md", "after\n")
	before := f.index()

	ctx := context.Background()
	tree, err := CaptureTree(ctx, f.repo, []string{"edit.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	finish, err := InstallTree(ctx, f.repo, tree)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(f.index(), before) {
		t.Fatal("InstallTree did not change the index")
	}
	if err := finish(false); err != nil {
		t.Fatalf("finish(false): %v", err)
	}
	if !bytes.Equal(f.index(), before) {
		t.Fatal("a failed queue write must put back the exact index the user had")
	}
	f.assertNoIndexBackups()
}

func TestInstallTreeLeavesAnIndexChangedSinceInstall(t *testing.T) {
	f := newCaptureFixture(t)
	f.seed()
	f.write("edit.md", "after\n")
	ctx := context.Background()
	tree, err := CaptureTree(ctx, f.repo, []string{"edit.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	finish, err := InstallTree(ctx, f.repo, tree)
	if err != nil {
		t.Fatal(err)
	}
	f.write("other.md", "staged by the user meanwhile\n")
	f.git("add", "other.md")
	if err := finish(false); err == nil {
		t.Fatal("finish(false) must refuse to overwrite an index another writer changed")
	}
	if staged := f.git("diff", "--cached", "--name-only"); !strings.Contains(staged, "other.md") {
		t.Fatalf("the other writer's staged file was lost; staged:\n%s", staged)
	}
	f.assertNoIndexBackups()
}

func TestOperationInProgressSeesMergesAndConflicts(t *testing.T) {
	f := newCaptureFixture(t)
	f.seed()
	ctx := context.Background()
	if busy, err := OperationInProgress(ctx, f.repo); err != nil || busy {
		t.Fatalf("clean repo: busy = %v, err = %v", busy, err)
	}
	for _, marker := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "REBASE_HEAD"} {
		path := filepath.Join(f.repo, ".git", marker)
		if err := os.WriteFile(path, []byte("0000000000000000000000000000000000000000\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if busy, err := OperationInProgress(ctx, f.repo); err != nil || !busy {
			t.Fatalf("%s: busy = %v, err = %v", marker, busy, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"rebase-merge", "rebase-apply"} {
		path := filepath.Join(f.repo, ".git", dir)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if busy, err := OperationInProgress(ctx, f.repo); err != nil || !busy {
			t.Fatalf("%s: busy = %v, err = %v", dir, busy, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}

	f.git("checkout", "-q", "-b", "side")
	f.write("edit.md", "side\n")
	f.git("commit", "-q", "-am", "side")
	f.git("checkout", "-q", "-")
	f.write("edit.md", "main\n")
	f.git("commit", "-q", "-am", "main")
	cmd := exec.Command("git", "-C", f.repo, "merge", "side")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected a conflicting merge:\n%s", out)
	}
	if err := os.Remove(filepath.Join(f.repo, ".git", "MERGE_HEAD")); err != nil {
		t.Fatal(err)
	}
	if busy, err := OperationInProgress(ctx, f.repo); err != nil || !busy {
		t.Fatalf("unmerged entries without a marker: busy = %v, err = %v", busy, err)
	}
}

func TestPlanRootContentSizeDecides(t *testing.T) {
	f := newCaptureFixture(t)
	f.write("videos/my-video/script.md", "shot list\n")
	f.write(".gitattributes", "videos/my-video/*.bin filter=lfs diff=lfs merge=lfs -text\n")
	f.git("add", ".")
	f.git("commit", "-q", "-m", "seed")

	big := strings.Repeat("x", 4096)
	f.write("videos/my-video/todo.md", "small\n")
	f.write("videos/my-video/drafts/idea.md", "small too\n")
	f.write("videos/my-video/drafts/take.mov", big)
	f.write("videos/my-video/broll.mp4", big)
	f.write("videos/my-video/allowed.wav", big)
	f.write("videos/my-video/model.bin", big)
	f.write("videos/my-video/nested/inner.md", "nested\n")
	nested := exec.Command("git", "-C", filepath.Join(f.repo, "videos/my-video/nested"), "init", "-q")
	if out, err := nested.CombinedOutput(); err != nil {
		t.Fatalf("init nested: %v\n%s", err, out)
	}

	ctx := context.Background()
	roots := []string{"videos/my-video"}
	limits := &stageguard.GuardLimits{
		MaxFileSize: 1024,
		LargeFiles:  stageguard.ModeAuto,
		Allow:       []string{"*.wav"},
	}
	plan, err := PlanRootContent(ctx, f.repo, roots, limits, false)
	if err != nil {
		t.Fatal(err)
	}
	commit := plan.CommitPaths()
	sort.Strings(commit)
	want := []string{
		"videos/my-video/allowed.wav",
		"videos/my-video/drafts/idea.md",
		"videos/my-video/model.bin",
		"videos/my-video/todo.md",
	}
	if !reflect.DeepEqual(commit, want) {
		t.Fatalf("commit = %v, want %v", commit, want)
	}
	var kept []string
	for _, c := range plan.Kept {
		kept = append(kept, c.Path)
	}
	sort.Strings(kept)
	if !reflect.DeepEqual(kept, []string{"videos/my-video/broll.mp4", "videos/my-video/drafts/take.mov"}) {
		t.Fatalf("kept = %v", kept)
	}
	if !reflect.DeepEqual(plan.KeptDirs, []string{"videos/my-video/nested"}) {
		t.Fatalf("kept dirs = %v, a nested repository stays out whole", plan.KeptDirs)
	}
	tally := plan.Tally(roots)
	if len(tally) != 1 || tally[0].Files != 2 || tally[0].Bytes != 2*4096 || tally[0].Dirs != 1 {
		t.Fatalf("tally = %+v", tally)
	}

	unknown, err := PlanRootContent(ctx, f.repo, roots, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown.Commit) != 0 || len(unknown.Kept) != 6 {
		t.Fatalf("an unreadable policy must keep everything out; commit = %v, kept = %d", unknown.CommitPaths(), len(unknown.Kept))
	}

	// The mode decides as it does for camp status: block keeps the same
	// files out as auto so its caller can refuse them; off and
	// --commit-large keep nothing out.
	block := *limits
	block.LargeFiles = stageguard.ModeBlock
	blocked, err := PlanRootContent(ctx, f.repo, roots, &block, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Kept) != 2 || len(blocked.KeptViolations()) != 2 {
		t.Fatalf("block: kept = %d, violations = %+v, want both over-threshold files", len(blocked.Kept), blocked.KeptViolations())
	}
	off := *limits
	off.LargeFiles = stageguard.ModeOff
	for name, run := range map[string]func() (RootPlan, error){
		"off":          func() (RootPlan, error) { return PlanRootContent(ctx, f.repo, roots, &off, false) },
		"commit-large": func() (RootPlan, error) { return PlanRootContent(ctx, f.repo, roots, limits, true) },
	} {
		got, err := run()
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Kept) != 0 || len(got.Commit) != 6 {
			t.Fatalf("%s: commit = %v, kept = %d, want every file committed", name, got.CommitPaths(), len(got.Kept))
		}
	}
}

func TestCaptureTreeAddsPathsLiterally(t *testing.T) {
	f := newCaptureFixture(t)
	f.seed()
	f.write("*.md", "a file named like a glob\n")
	f.write("other.md", "must not be swept in\n")
	tree, err := CaptureTree(context.Background(), f.repo, []string{"*.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed := f.git("ls-tree", "--name-only", tree)
	if !strings.Contains(listed, "*.md\n") || strings.Contains(listed, "other.md") {
		t.Fatalf("tree must hold the literal file only:\n%s", listed)
	}
}

func TestRootExcludePathspecsSkipsIgnoredRoots(t *testing.T) {
	f := newCaptureFixture(t)
	f.write(".gitignore", "media/\n")
	f.write("mixed/script.md", "tracked\n")
	f.git("add", ".")
	f.git("commit", "-q", "-m", "seed")
	f.write("media/seed.bin", "clean root content\n")

	ctx := context.Background()
	got, err := rootExcludePathspecs(ctx, f.repo, []string{"media/", "mixed"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"mixed"}) {
		t.Fatalf("excludes = %v, want only the root git does not ignore", got)
	}
	if _, err := rootExcludePathspecs(ctx, t.TempDir(), []string{"media"}); err == nil {
		t.Fatal("a check-ignore failure must stop the stage, not exclude blindly")
	}
}

func TestPairMovesIntoRootsNamesTrackedFilesPastTheLimit(t *testing.T) {
	f := newCaptureFixture(t)
	big := strings.Repeat("model weights\n", 400)
	f.write("notes/model.bin", big)
	f.write("notes/a.md", strings.Repeat("carry me\n", 40))
	f.write("videos/my-video/take.mov", big+"take\n")
	f.git("add", ".")
	f.git("commit", "-q", "-m", "seed")

	if err := os.Rename(filepath.Join(f.repo, "notes/model.bin"), filepath.Join(f.repo, "videos/my-video/model.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(f.repo, "videos/my-video/take.mov"), filepath.Join(f.repo, "videos/my-video/take-final.mov")); err != nil {
		t.Fatal(err)
	}
	f.write("videos/my-video/broll.mp4", strings.Repeat("\x00", len(big)))
	if err := os.Rename(filepath.Join(f.repo, "notes/a.md"), filepath.Join(f.repo, "notes/b.md")); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	limits := &stageguard.GuardLimits{MaxFileSize: 1024}
	plan, err := PlanRootContent(ctx, f.repo, []string{"videos/my-video"}, limits, false)
	if err != nil {
		t.Fatal(err)
	}
	before := f.index()
	deleted := []string{"notes/a.md", "notes/model.bin", "videos/my-video/take.mov"}
	moves, err := PairMovesIntoRoots(ctx, f.repo, plan.Kept, deleted, []string{"notes/b.md"})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(moves, func(i, j int) bool { return moves[i].From < moves[j].From })
	want := []RootMove{
		{From: "notes/model.bin", To: "videos/my-video/model.bin", Size: int64(len(big))},
		{From: "videos/my-video/take.mov", To: "videos/my-video/take-final.mov", Size: int64(len(big) + 5)},
	}
	if !reflect.DeepEqual(moves, want) {
		t.Fatalf("moves = %+v, want %+v (unrelated media of the same size must not pair)", moves, want)
	}
	if !bytes.Equal(f.index(), before) {
		t.Fatal("pairing must not touch the real index")
	}
	plan.Moved = moves
	if tally := plan.Tally([]string{"videos/my-video"}); len(tally) != 1 || tally[0].Files != 1 {
		t.Fatalf("tally = %+v, want only the b-roll counted as media", tally)
	}

	none, err := PairMovesIntoRoots(ctx, f.repo, plan.Kept, []string{"notes/a.md"}, []string{"notes/b.md"})
	if err != nil || len(none) != 0 {
		t.Fatalf("a deleted file no kept file can match: moves = %+v, err = %v", none, err)
	}
}

func TestInstallTreeDetectsAChangedIndexWithoutAChecksum(t *testing.T) {
	f := newCaptureFixture(t)
	f.git("config", "index.skipHash", "true")
	f.seed()
	f.write("edit.md", "after\n")
	ctx := context.Background()
	tree, err := CaptureTree(ctx, f.repo, []string{"edit.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	finish, err := InstallTree(ctx, f.repo, tree)
	if err != nil {
		t.Fatal(err)
	}
	data := f.index()
	if !bytes.Equal(data[len(data)-20:], make([]byte, 20)) {
		t.Skip("this git does not honor index.skipHash; the checksum path is covered elsewhere")
	}
	f.write("other.md", "staged by the user meanwhile\n")
	f.git("add", "other.md")
	if err := finish(false); err == nil {
		t.Fatal("an index with a zero checksum must still be compared by content")
	}
	if staged := f.git("diff", "--cached", "--name-only"); !strings.Contains(staged, "other.md") {
		t.Fatalf("the other writer's staged file was lost; staged:\n%s", staged)
	}
}

// A directory link inside a declared root is committed as its link blob,
// never as the files in its target, on the foreground root staging path.
func TestStageAllCommitsADirectoryLinkInsideARootAsItsLink(t *testing.T) {
	lf := newLinkedGitFixture(t)
	if err := os.MkdirAll(filepath.Join(lf.repo, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	declared := "version: 1\nroots:\n  - path: projects\n"
	if err := os.WriteFile(filepath.Join(lf.repo, ".campaign", "artifacts.yaml"), []byte(declared), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lf.repo, "projects", "notes.md"), []byte("small\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	plan, err := PlanRootContent(ctx, lf.repo, []string{"projects"}, &stageguard.GuardLimits{MaxFileSize: 1 << 20, LargeFiles: stageguard.ModeAuto}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Links, []string{"projects/linked"}) {
		t.Fatalf("links = %v, want the directory link itself", plan.Links)
	}
	for _, path := range plan.CommitPaths() {
		if strings.HasPrefix(path, "projects/linked/") {
			t.Fatalf("a file under the link was planned: %s", path)
		}
	}
	if err := StageAll(ctx, lf.repo); err != nil {
		t.Fatalf("StageAll: %v", err)
	}
	entry := lf.git(lf.repo, "ls-files", "--stage", "--", "projects/linked")
	if !strings.HasPrefix(entry, "120000 ") {
		t.Fatalf("index entry = %q, want one mode 120000 link", entry)
	}
	if children := lf.git(lf.repo, "ls-files", "--", "projects/linked/"); children != "" {
		t.Fatalf("files from the link's target were staged:\n%s", children)
	}
	if staged := lf.git(lf.repo, "ls-files", "--", "projects/notes.md"); !strings.Contains(staged, "projects/notes.md") {
		t.Fatal("a small file beside the link must still be staged")
	}
}
