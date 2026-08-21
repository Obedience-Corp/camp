package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Report is the host-fs ratchet result for one tree.
type Report struct {
	Hits []string
	New  []string
}

var (
	directGitRe = regexp.MustCompile(`exec\.Command\("git"|exec\.CommandContext\([^)]*"git"|exec\.Command\(args\[0\]`)
	funcNameRe  = regexp.MustCompile(`(?m)^func ([A-Za-z][A-Za-z0-9]*)\(`)
)

type testFile struct {
	rel         string
	dir         string
	containerFS bool
	directGit   bool
	helperNames []string
	content     []byte
}

// Scan walks root for host-side git-mutating tests. Files under
// tests/integration/, vendor/, testdata/, and .git/ are ignored.
// container_fs-tagged files are compliant. Remaining hits not on
// allowlist are Report.New.
func Scan(ctx context.Context, root string, allowlist []string) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, camperrors.Wrap(err, "hostfslint: resolve root")
	}
	files, err := collectTestFiles(ctx, root)
	if err != nil {
		return Report{}, err
	}
	dirHelpers := helpersByDir(files)
	allowed := allowlistSet(allowlist)
	hits := make([]string, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		if f.containerFS {
			continue
		}
		if f.directGit || callsDirHelper(f, dirHelpers[f.dir]) {
			hits = append(hits, f.rel)
		}
	}
	sort.Strings(hits)
	var fresh []string
	for _, hit := range hits {
		if _, ok := allowed[hit]; !ok {
			fresh = append(fresh, hit)
		}
	}
	return Report{Hits: hits, New: fresh}, nil
}

func collectTestFiles(ctx context.Context, root string) ([]*testFile, error) {
	var files []*testFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, relErr := relSlash(root, path)
		if relErr != nil {
			return camperrors.Wrap(relErr, "hostfslint: relative path")
		}
		if d.IsDir() {
			return skipWalkDir(d.Name(), rel)
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		tf, loadErr := loadTestFile(path, rel)
		if loadErr != nil {
			return loadErr
		}
		files = append(files, tf)
		return nil
	})
	if err != nil {
		return nil, camperrors.Wrap(err, "hostfslint: walk tests")
	}
	return files, nil
}

func skipWalkDir(name, rel string) error {
	switch name {
	case "vendor", "testdata", ".git":
		return fs.SkipDir
	}
	if name == "integration" && strings.HasSuffix(path.Dir(rel), "/tests") {
		return fs.SkipDir
	}
	if rel == "./tests/integration" {
		return fs.SkipDir
	}
	return nil
}

func loadTestFile(filePath, rel string) (*testFile, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, camperrors.Wrapf(err, "hostfslint: read %s", rel)
	}
	tf := &testFile{
		rel:         rel,
		dir:         path.Dir(rel),
		containerFS: hasContainerFSTag(content),
		directGit:   directGitRe.Match(content),
		content:     content,
	}
	if tf.directGit {
		tf.helperNames = gitHelperNames(content)
	}
	return tf, nil
}

func helpersByDir(files []*testFile) map[string][]string {
	seen := make(map[string]map[string]struct{})
	for _, f := range files {
		if len(f.helperNames) == 0 {
			continue
		}
		if seen[f.dir] == nil {
			seen[f.dir] = make(map[string]struct{})
		}
		for _, name := range f.helperNames {
			seen[f.dir][name] = struct{}{}
		}
	}
	out := make(map[string][]string, len(seen))
	for dir, names := range seen {
		list := make([]string, 0, len(names))
		for name := range names {
			list = append(list, name)
		}
		sort.Strings(list)
		out[dir] = list
	}
	return out
}

func callsDirHelper(f *testFile, helpers []string) bool {
	if len(helpers) == 0 {
		return false
	}
	for _, name := range helpers {
		re := helperCallRegexp(name)
		if re.Match(f.content) {
			return true
		}
	}
	return false
}

func helperCallRegexp(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\(`)
}

func gitHelperNames(content []byte) []string {
	matches := funcNameRe.FindAllSubmatch(content, -1)
	var names []string
	for _, m := range matches {
		name := string(m[1])
		if isTestFunc(name) || !isGitHelperName(name) {
			continue
		}
		names = append(names, name)
	}
	return names
}

func isTestFunc(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isGitHelperName(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "git") || strings.Contains(lower, "repo") || strings.Contains(lower, "submodule") {
		return true
	}
	switch name {
	case "mustRunCmd", "runCmd":
		return true
	default:
		return false
	}
}

func hasContainerFSTag(content []byte) bool {
	lines := bytes.SplitN(content, []byte("\n"), 4)
	n := 3
	if len(lines) < n {
		n = len(lines)
	}
	for i := 0; i < n; i++ {
		line := strings.TrimSpace(string(lines[i]))
		if !strings.Contains(line, "container_fs") {
			continue
		}
		if strings.HasPrefix(line, "//go:build") || strings.HasPrefix(line, "// +build") {
			return true
		}
	}
	return false
}

func relSlash(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return ".", nil
	}
	return "./" + rel, nil
}

func allowlistSet(allowlist []string) map[string]struct{} {
	out := make(map[string]struct{}, len(allowlist))
	for _, p := range allowlist {
		out[filepath.ToSlash(p)] = struct{}{}
	}
	return out
}
