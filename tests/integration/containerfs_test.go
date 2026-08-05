//go:build integration
// +build integration

package integration

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// In-container execution of filesystem-mutating package tests (decision D007).
//
// The standing rule is that tests which create, move, delete, or git-init real
// files must not do it on a developer's machine. Until now the only way to
// satisfy it was to relocate a test into package integration and drive the camp
// binary — which works for behavioural tests and is impossible for the ones
// that assert on unexported package internals. Two thirds of the internal/clone
// and internal/sync suites are the second kind, so relocation would have
// deleted the assertions rather than moved them.
//
// This runs those suites where they are. Each package's FS-mutating tests carry
// the container_fs build tag, so a plain `just test` on the host does not
// compile them at all; the test binary is cross-compiled here, copied into a
// pooled container, and executed against that container's disposable
// filesystem. The tests keep package scope, keep their assertions verbatim, and
// still cannot touch the host.
//
// The tag is the enforcement seam and the copy is the isolation. Neither works
// alone: a tag on its own would merely hide the tests from every runner, which
// is why containerFSPackages asserts a minimum test count from the output —
// silently running nothing is the failure mode this design must not have.

// containerFSPackage is one package whose FS-mutating tests run in a container.
type containerFSPackage struct {
	// ImportPath is the package to compile, relative to the module root.
	ImportPath string
	// MinTests is the number of top-level tests the binary must report running.
	// It is a floor rather than an exact count so adding a test does not break
	// the harness, but it is high enough that a tag misconfiguration — which
	// would run zero — fails loudly instead of passing silently.
	MinTests int
}

// containerFSPackages are the suites migrated under D007. The counts are the
// tests actually behind the tag at migration time.
var containerFSPackages = []containerFSPackage{
	{ImportPath: "./internal/clone", MinTests: 29},
	{ImportPath: "./internal/sync", MinTests: 32},
}

// containerFSTag is the build tag gating host compilation of these suites.
const containerFSTag = "container_fs"

// buildContainerFSBinary cross-compiles one package's tagged test binary for the
// pooled container: linux, the container's architecture, and CGO off because
// the pool images are alpine/musl and a glibc-linked binary would not start.
func buildContainerFSBinary(pkg containerFSPackage, outDir string) (string, error) {
	name := strings.ReplaceAll(strings.TrimPrefix(pkg.ImportPath, "./"), "/", "_") + ".test"
	out := filepath.Join(outDir, name)

	cmd := exec.Command("go", "test", "-c",
		"-tags", containerFSTag,
		"-o", out,
		pkg.ImportPath)
	cmd.Dir = repoRootForContainerFS()
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH="+containerArch(),
		"CGO_ENABLED=0",
	)
	if combined, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("compile %s test binary: %w: %s", pkg.ImportPath, err, combined)
	}
	return out, nil
}

// containerArch returns the architecture the pooled containers run, which is the
// host's: the pool uses the local Docker engine, not an emulated one.
func containerArch() string {
	out, err := exec.Command("go", "env", "GOARCH").Output()
	if err != nil {
		return "arm64"
	}
	return strings.TrimSpace(string(out))
}

// repoRootForContainerFS returns the module root, since this file lives two
// directories below it.
func repoRootForContainerFS() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return filepath.Join(wd, "..", "..")
}

// runTestBinaryPattern counts the top-level tests a Go test binary reported.
// Subtests carry a slash, so anchoring on end-of-line counts each parent once.
var runTestBinaryPattern = regexp.MustCompile(`(?m)^=== RUN\s+(Test[A-Za-z0-9_]+)$`)

// RunContainerFSSuite compiles one package's tagged tests, copies the binary
// into the container, runs it there, and fails the test with the binary's own
// output when it does not pass.
//
// The binary runs with its working directory inside the container, so every
// t.TempDir and git init it performs lands on the container's filesystem.
func RunContainerFSSuite(t *testing.T, tc *TestContainer, pkg containerFSPackage) {
	t.Helper()

	outDir, err := os.MkdirTemp("", "camp-containerfs-")
	require.NoError(t, err, "create build dir")
	defer func() { _ = os.RemoveAll(outDir) }()

	binPath, err := buildContainerFSBinary(pkg, outDir)
	require.NoError(t, err, "cross-compile %s", pkg.ImportPath)

	remote := "/containerfs/" + filepath.Base(binPath)
	tc.Shell(t, "mkdir -p /containerfs /containerfs/work")

	ctx, cancel := context.WithTimeout(tc.ctx, 10*time.Minute)
	defer cancel()
	require.NoError(t, tc.container.CopyFileToContainer(ctx, binPath, remote, 0o755),
		"copy %s into the container", remote)

	// git needs an identity and a permissive safe.directory for the repos these
	// tests create. HOME is exported before `git config --global` runs, not just
	// for the binary: --global writes to $HOME/.gitconfig, so configuring it
	// under root's HOME and then running the tests under a different one leaves
	// git with no identity and every commit fails with "please tell me who you
	// are".
	out, exit, execErr := tc.ExecCommand("sh", "-c", strings.Join([]string{
		"export HOME=/containerfs/work",
		"export TMPDIR=/containerfs/work",
		"cd /containerfs/work",
		"git config --global user.email t@t.co",
		"git config --global user.name T",
		"git config --global --add safe.directory '*'",
		remote + " -test.v -test.count=1",
	}, " && "))
	require.NoError(t, execErr, "exec %s in container", remote)

	ran := runTestBinaryPattern.FindAllStringSubmatch(out, -1)
	if exit != 0 {
		t.Fatalf("%s container suite failed (exit %d), %d tests ran:\n%s",
			pkg.ImportPath, exit, len(ran), failureExcerpt(out))
	}

	// A tag that excluded the tests from everywhere would leave a passing,
	// empty run. Assert they actually executed.
	require.GreaterOrEqualf(t, len(ran), pkg.MinTests,
		"%s ran %d top-level tests in the container, want at least %d — a tag "+
			"misconfiguration would run zero and still exit 0",
		pkg.ImportPath, len(ran), pkg.MinTests)

	t.Logf("%s: %d top-level tests executed in the container", pkg.ImportPath, len(ran))
}

// failureExcerpt pulls the failing tests and their messages out of a verbose Go
// test log. Showing the tail is wrong here: `-test.v` interleaves output, so the
// last lines are usually whichever test happened to finish last, not the one
// that failed.
func failureExcerpt(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var picked []string
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "--- FAIL:") && !strings.HasPrefix(trimmed, "FAIL\t") {
			continue
		}
		picked = append(picked, l)
		// The assertion text sits between the preceding RUN and this FAIL.
		for j := i - 1; j >= 0 && j > i-12; j-- {
			prev := strings.TrimSpace(lines[j])
			if strings.HasPrefix(prev, "=== RUN") || strings.HasPrefix(prev, "--- ") {
				break
			}
			picked = append(picked, "    "+prev)
		}
		if len(picked) > 80 {
			break
		}
	}
	if len(picked) == 0 {
		return "(no --- FAIL lines found; full tail)\n" + tailLines(out, 40)
	}
	return strings.Join(picked, "\n")
}

// tailLines is the fallback when no failure marker is present.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return "…\n" + strings.Join(lines[len(lines)-n:], "\n")
}

// TestContainerFSPackagesAreAllRegistered closes the one hole the tag opens.
//
// The tag keeps a file off the host; containerFSPackages is what makes it run
// somewhere. Tag a file and forget to register its package and those tests run
// NOWHERE — the host skips them and no lane entry compiles them — while every
// suite stays green. That is a worse outcome than the host-FS problem the tag
// was introduced to fix, because it is silent.
//
// The per-package minimum count in RunContainerFSSuite cannot catch it: it only
// checks packages already registered. This walks the tree instead and fails on
// any tagged file whose package is absent from the list.
func TestContainerFSPackagesAreAllRegistered(t *testing.T) {
	root := repoRootForContainerFS()

	registered := make(map[string]bool, len(containerFSPackages))
	for _, p := range containerFSPackages {
		registered[path.Clean(strings.TrimPrefix(p.ImportPath, "./"))] = true
	}

	tagged := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		if !fileHasContainerFSTag(p) {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(p))
		if relErr != nil {
			return relErr
		}
		pkg := filepath.ToSlash(rel)
		tagged[pkg] = append(tagged[pkg], d.Name())
		return nil
	})
	require.NoError(t, err, "walk module root")

	require.NotEmpty(t, tagged,
		"no %s-tagged files found at all; either the tag was removed or this "+
			"check is looking in the wrong place", containerFSTag)

	for pkg, files := range tagged {
		require.Truef(t, registered[pkg],
			"package %q has %s-tagged test files (%s) but is not in "+
				"containerFSPackages, so those tests run nowhere: the host skips "+
				"them and no lane entry compiles them. Add {ImportPath: \"./%s\"} "+
				"with a MinTests floor.",
			pkg, containerFSTag, strings.Join(files, ", "), pkg)
	}

	// The converse: a registered package with no tagged files would run its
	// pure tests in a container for no reason and mask a lost migration.
	for pkg := range registered {
		require.Containsf(t, tagged, pkg,
			"package %q is registered in containerFSPackages but has no %s-tagged "+
				"files; either the migration was reverted or the entry is stale",
			pkg, containerFSTag)
	}

	t.Logf("%d package(s) carry %s files, all registered: %v",
		len(tagged), containerFSTag, keysOf(tagged))
}

// fileHasContainerFSTag reports whether a file declares the container_fs build
// constraint. Only the leading lines are read, matching the ratchet's own
// mechanical check so the two cannot disagree about what "tagged" means.
func fileHasContainerFSTag(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for i := 0; scanner.Scan() && i < 3; i++ {
		if strings.HasPrefix(strings.TrimSpace(scanner.Text()), "//go:build "+containerFSTag) {
			return true
		}
	}
	return false
}

// keysOf returns a map's keys for a log line.
func keysOf(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestContainerFSSuites runs every migrated package's FS-mutating tests inside
// a pooled container. This is the lane entry point for D007.
func TestContainerFSSuites(t *testing.T) {
	for _, pkg := range containerFSPackages {
		t.Run(strings.TrimPrefix(pkg.ImportPath, "./"), func(t *testing.T) {
			tc := GetSharedContainer(t)
			RunContainerFSSuite(t, tc, pkg)
		})
	}
}
