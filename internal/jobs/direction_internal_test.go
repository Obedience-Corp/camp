package jobs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/git"
	"strings"
	"testing"
)

const directionShim = "#!/bin/sh\n" +
	"# direction-hook v1 — appends Festival-Direction trailers; see docs/anchoring.md\n" +
	"here=\"$(dirname \"$0\")\"\n" +
	"if [ -x \"$here/commit-msg.before-direction\" ]; then \"$here/commit-msg.before-direction\" \"$@\" || exit $?; fi\n" +
	"exec fest-direction hook commit-msg \"$1\"\n"

func TestExecuteCommitTreeAppendsDirectionTrailers(t *testing.T) {
	repo, parent := seedRepo(t)
	tree := captureTree(t, repo, "changed\n")
	installCommitMsg(t, repo, directionShim)
	argsFile := filepath.Join(t.TempDir(), "args")
	stdinFile := filepath.Join(t.TempDir(), "stdin")
	isolatePath(t, map[string]string{
		"fest-direction": trailerScript(argsFile, stdinFile),
	})

	const message = "subject from the writer\n"
	job := &Job{
		ID:      "job-trailers",
		Kind:    KindCommitTree,
		Repo:    ".",
		Tree:    tree,
		Parent:  parent,
		Message: message,
	}
	if err := executeCommitTree(context.Background(), repo, repo, job); err != nil {
		t.Fatalf("executeCommitTree() error = %v", err)
	}

	body := gitOutput(t, repo, "log", "-1", "--format=%B")
	if !strings.Contains(body, "subject from the writer") || !strings.Contains(body, "Direction-Trailer: appended") {
		t.Fatalf("commit message = %q, want the writer's subject plus the trailer command's stdout", body)
	}
	if strings.TrimSpace(body) == "subject from the writer" {
		t.Fatal("commit message was not replaced with the trailer command's stdout")
	}
	args := readTestFile(t, argsFile)
	if args != "trailers\n--tree\n"+tree+"\n" {
		t.Fatalf("trailer args = %q, want trailers --tree %s", args, tree)
	}
	if stdin := readTestFile(t, stdinFile); stdin != message {
		t.Fatalf("trailer stdin = %q, want the built message %q", stdin, message)
	}
	if got := gitOutput(t, repo, "rev-parse", "HEAD^{tree}"); got != tree {
		t.Fatalf("HEAD tree = %s, want captured %s", got, tree)
	}
}

func TestExecuteCommitTreeDirectionFallbackBinary(t *testing.T) {
	repo, parent := seedRepo(t)
	tree := captureTree(t, repo, "via-direction\n")
	installCommitMsg(t, repo, "#!/bin/sh\n# direction-hook v1\nexec direction hook commit-msg \"$1\"\n")
	argsFile := filepath.Join(t.TempDir(), "args")
	stdinFile := filepath.Join(t.TempDir(), "stdin")
	isolatePath(t, map[string]string{
		"direction": trailerScript(argsFile, stdinFile),
	})

	job := &Job{
		ID: "job-direction-bin", Kind: KindCommitTree, Repo: ".",
		Tree: tree, Parent: parent, Message: "older binary\n",
	}
	if err := executeCommitTree(context.Background(), repo, repo, job); err != nil {
		t.Fatalf("executeCommitTree() error = %v", err)
	}
	body := gitOutput(t, repo, "log", "-1", "--format=%B")
	if !strings.Contains(body, "Direction-Trailer: appended") {
		t.Fatalf("commit message = %q, want trailers from the direction binary", body)
	}
}

func TestExecuteCommitTreeDirectionTrailerFailureSkipsCommit(t *testing.T) {
	repo, parent := seedRepo(t)
	tree := captureTree(t, repo, "will-not-land\n")
	installCommitMsg(t, repo, directionShim)
	isolatePath(t, map[string]string{
		"fest-direction": "#!/bin/sh\necho 'trailers refused' >&2\nexit 3\n",
	})
	before := looseObjectCount(t, repo)

	job := &Job{
		ID: "job-trailers-fail", Kind: KindCommitTree, Repo: ".",
		Tree: tree, Parent: parent, Message: "do not commit\n",
	}
	err := executeCommitTree(context.Background(), repo, repo, job)
	if err == nil {
		t.Fatal("executeCommitTree() succeeded; a failing trailer command must fail the job")
	}
	if !strings.Contains(err.Error(), "trailers refused") {
		t.Fatalf("error = %v, want the trailer command's failure", err)
	}
	if head := gitOutput(t, repo, "rev-parse", "HEAD"); head != parent {
		t.Fatalf("HEAD = %s, want %s; CommitTree's result was applied", head, parent)
	}
	if after := looseObjectCount(t, repo); after != before {
		t.Fatalf("loose objects = %d, was %d; CommitTree wrote an object before the trailer command failed", after, before)
	}
}

func TestExecuteCommitTreeMissingDirectionBinarySkipsCommit(t *testing.T) {
	repo, parent := seedRepo(t)
	tree := captureTree(t, repo, "no-binary\n")
	installCommitMsg(t, repo, directionShim)
	isolatePath(t, nil)
	before := looseObjectCount(t, repo)

	job := &Job{
		ID: "job-no-binary", Kind: KindCommitTree, Repo: ".",
		Tree: tree, Parent: parent, Message: "do not commit\n",
	}
	err := executeCommitTree(context.Background(), repo, repo, job)
	if err == nil || !strings.Contains(err.Error(), "neither fest-direction nor direction is on PATH") {
		t.Fatalf("error = %v, want a missing-binary failure", err)
	}
	if head := gitOutput(t, repo, "rev-parse", "HEAD"); head != parent {
		t.Fatalf("HEAD = %s, want %s", head, parent)
	}
	if after := looseObjectCount(t, repo); after != before {
		t.Fatalf("loose objects = %d, was %d; the commit was created without trailers", after, before)
	}
}

func TestExecuteCommitTreeWithoutShimSkipsTrailerCommand(t *testing.T) {
	repo, parent := seedRepo(t)
	tree := captureTree(t, repo, "plain\n")
	logFile := filepath.Join(t.TempDir(), "invocations")
	isolatePath(t, map[string]string{
		"fest-direction": "#!/bin/sh\necho called >> " + shellQuote(logFile) + "\nexit 1\n",
	})

	const message = "no shim, no trailers\n"
	job := &Job{
		ID: "job-no-shim", Kind: KindCommitTree, Repo: ".",
		Tree: tree, Parent: parent, Message: message,
	}
	if err := executeCommitTree(context.Background(), repo, repo, job); err != nil {
		t.Fatalf("executeCommitTree() error = %v; a repo with no hooks must still commit", err)
	}
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Fatalf("trailer command was run (stat %v); a repo with no hooks must not run it", err)
	}
	body := gitOutput(t, repo, "log", "-1", "--format=%B")
	if !strings.Contains(body, "no shim, no trailers") || strings.Contains(body, "Direction-Trailer") {
		t.Fatalf("commit message = %q, want the built message unchanged", body)
	}
}

// A paths job commits with git commit, which runs the real hook. The worker
// must not also invoke `trailers --tree`, or the trailers would be applied twice.
func TestExecuteCommitPathsDoesNotAppendDirectionTrailers(t *testing.T) {
	repo, _ := seedRepo(t)
	installCommitMsg(t, repo, directionShim)
	blob := writeBlob(t, repo, "captured\n")
	logFile := filepath.Join(t.TempDir(), "invocations")
	isolatePath(t, map[string]string{
		"fest-direction": pathsHookScript(logFile),
	})

	job := &Job{
		ID:      "job-paths",
		Kind:    KindCommitPaths,
		Repo:    ".",
		Paths:   []string{"note.md"},
		Blobs:   []BlobRef{{Path: "note.md", Mode: "100644", SHA: blob}},
		Message: "bookkeeping\n",
	}
	if err := executeCommitPaths(context.Background(), repo, job); err != nil {
		t.Fatalf("executeCommitPaths() error = %v", err)
	}
	log := readTestFile(t, logFile)
	if !strings.Contains(log, "hook\ncommit-msg\n") {
		t.Fatalf("hook log = %q, want the real commit-msg hook to have run", log)
	}
	if strings.Contains(log, "trailers") {
		t.Fatalf("hook log = %q, want no trailers command; git commit already ran the shim", log)
	}
}

func seedRepo(t *testing.T) (repo, parent string) {
	t.Helper()
	repo = t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "config", "user.email", "test@test.com")
	gitRun(t, repo, "config", "user.name", "Test")
	gitRun(t, repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-q", "-m", "seed")
	return repo, gitOutput(t, repo, "rev-parse", "HEAD")
}

func captureTree(t *testing.T, repo, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "README.md")
	return gitOutput(t, repo, "write-tree")
}

func installCommitMsg(t *testing.T, repo, body string) {
	t.Helper()
	path := filepath.Join(repo, ".git", "hooks", "commit-msg")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func isolatePath(t *testing.T, scripts map[string]string) {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(gitBin, filepath.Join(dir, "git")); err != nil {
		t.Fatal(err)
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func trailerScript(argsFile, stdinFile string) string {
	// printf and read only. The test replaces PATH, so external commands such
	// as cat are not there, and the script must still echo stdin back.
	return fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > %s
: > %s
while IFS= read -r line || [ -n "$line" ]; do
	printf '%%s\n' "$line" >> %s
	printf '%%s\n' "$line"
done
printf 'Direction-Trailer: appended\n'
`, shellQuote(argsFile), shellQuote(stdinFile), shellQuote(stdinFile))
}

func pathsHookScript(logFile string) string {
	return fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >> %s\nif [ \"$1\" = trailers ]; then echo trailers >&2; exit 1; fi\nexit 0\n",
		shellQuote(logFile))
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func writeBlob(t *testing.T, repo, content string) string {
	t.Helper()
	path := filepath.Join(repo, ".direction-blob")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return gitOutput(t, repo, "hash-object", "-w", "--", ".direction-blob")
}

func gitRun(t *testing.T, repo string, args ...string) {
	t.Helper()
	gitOutput(t, repo, args...)
}

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := git.Output(context.Background(), repo, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func looseObjectCount(t *testing.T, repo string) int {
	t.Helper()
	n := 0
	root := filepath.Join(repo, ".git", "objects")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// info and pack are not commit objects commit-tree creates.
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if strings.HasPrefix(rel, "info"+string(filepath.Separator)) || strings.HasPrefix(rel, "pack"+string(filepath.Separator)) {
			return nil
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("walk objects: %v", err)
	}
	return n
}
