package git

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The staging guard covers camp with a single edit only because every
// stage-everything operation reaches executeStage. That is a structural
// property, not a documented intention: a new `git add` anywhere else is
// silently unguarded, and nothing else in the build would notice.
//
// Design doc 01 asserted this property, and the assertion was already wrong
// when written — internal/project/new.go ran a bare `git add .` for five
// months before the guard existed. This test is what makes the claim true
// going forward.
//
// The rule: every `git add` invocation in production code lives in this
// package. internal/git/commit.go holds three, each deliberate:
//
//   - executeStage, the guarded chokepoint
//   - AddPathsToTempIndex, explicit named paths into a temp index
//   - StageTrackedChanges (`git add -u`), which cannot introduce a new path
//
// A staging invocation names "add" alongside the git binary or its -C flag in
// the same argument list. Requiring that companion token keeps cobra command
// declarations (`Use: "add"`) and unrelated "add" constants out of the match.
var gitAddInvocation = regexp.MustCompile(`"add"[,)\s]`)

// hasGitArgContext reports whether a line carries the rest of a git argument
// list, distinguishing a real invocation from an incidental "add" literal.
func hasGitArgContext(line string) bool {
	return strings.Contains(line, `"git"`) || strings.Contains(line, `"-C"`)
}

func TestNoStagingPathBypassesTheFunnel(t *testing.T) {
	root := repoRootForTest(t)

	// Invocations of other git subcommands that merely take an "add" literal
	// argument are not staging: `remote add`, `submodule add`, `worktree add`.
	notStaging := []string{
		`"remote", "add"`,
		`"submodule", "add"`,
		`"worktree", "add"`,
		`NewGit("add"`,
	}

	var offenders []string
	for _, dir := range []string{"cmd", "internal", "pkg"} {
		walkErr := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			// This package is where staging is allowed to live.
			if strings.HasPrefix(rel, "internal/git/") {
				return nil
			}

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, line := range strings.Split(string(data), "\n") {
				if !gitAddInvocation.MatchString(line) || !hasGitArgContext(line) {
					continue
				}
				if isAllowed(line, notStaging) {
					continue
				}
				offenders = append(offenders, rel+": "+strings.TrimSpace(line))
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", dir, walkErr)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("git add outside internal/git bypasses the staging guard:\n  %s\n\n"+
			"Route staging through git.Stage (or StageWithGuard to report what the guard did). "+
			"If the call genuinely is not staging, extend the allowlist in this test.",
			strings.Join(offenders, "\n  "))
	}
}

func isAllowed(line string, allowed []string) bool {
	for _, a := range allowed {
		if strings.Contains(line, a) {
			return true
		}
	}
	return false
}

// repoRootForTest walks up from the working directory to the module root.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the working directory")
		}
		dir = parent
	}
}
