package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrePushClearsGitHookEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pre-push hook is a POSIX shell script")
	}

	repoRoot := campRepoRoot(t)
	repo := t.TempDir()
	run(t, "", "git", "init", repo)

	binDir := t.TempDir()
	justPath := filepath.Join(binDir, "just")
	fakeJust := `#!/usr/bin/env sh
set -eu

if [ "$*" != "gate-push" ]; then
	echo "unexpected just args: $*" >&2
	exit 2
fi

for name in GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX; do
	eval "value=\${$name-}"
	if [ -n "$value" ]; then
		echo "$name leaked into gate command: $value" >&2
		exit 10
	fi
done
`
	if err := os.WriteFile(justPath, []byte(fakeJust), 0o755); err != nil {
		t.Fatalf("write fake just: %v", err)
	}

	cmd := exec.Command("sh", ".githooks/pre-push")
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader("refs/heads/main 0000000000000000000000000000000000000000 refs/heads/main 0000000000000000000000000000000000000000\n")
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_DIR="+filepath.Join(repo, ".git"),
		"GIT_WORK_TREE="+repo,
		"GIT_INDEX_FILE="+filepath.Join(t.TempDir(), "index"),
		"GIT_PREFIX=hooks/",
		"CAMP_GATE_FAST=",
		"CAMP_GATE_FULL=",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pre-push hook failed: %v\n%s", err, out)
	}
}

func campRepoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}
