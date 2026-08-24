package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParsePushArgs(t *testing.T) {
	cases := []struct {
		name            string
		args            []string
		wantRemote      string
		wantBranch      string
		wantExtras      []string
		wantEnqueueable bool
	}{
		{name: "no args", args: nil, wantEnqueueable: true},
		{name: "remote only", args: []string{"origin"}, wantRemote: "origin", wantEnqueueable: true},
		{name: "remote and branch", args: []string{"origin", "main"}, wantRemote: "origin", wantBranch: "main", wantEnqueueable: true},
		{name: "force", args: []string{"--force"}, wantExtras: []string{"--force"}},
		{name: "force-with-lease and target", args: []string{"--force-with-lease", "origin", "main"}, wantRemote: "origin", wantBranch: "main", wantExtras: []string{"--force-with-lease"}},
		{name: "tags", args: []string{"--tags"}, wantExtras: []string{"--tags"}},
		{name: "set-upstream", args: []string{"-u", "origin", "feature"}, wantRemote: "origin", wantBranch: "feature", wantExtras: []string{"-u"}},
		{name: "extra refspec", args: []string{"origin", "main", "other"}, wantRemote: "origin", wantBranch: "main", wantExtras: []string{"other"}},
		{name: "refspec mapping", args: []string{"origin", "main:other"}, wantRemote: "origin", wantExtras: []string{"main:other"}},
		{name: "force refspec", args: []string{"origin", "+main"}, wantRemote: "origin", wantExtras: []string{"+main"}},
		{name: "dash-dash separator", args: []string{"--", "origin", "main"}, wantRemote: "origin", wantBranch: "main", wantExtras: []string{"--"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remote, branch, extras := parsePushArgs(tc.args)
			if remote != tc.wantRemote || branch != tc.wantBranch {
				t.Errorf("remote/branch = %q/%q, want %q/%q", remote, branch, tc.wantRemote, tc.wantBranch)
			}
			if !slices.Equal(extras, tc.wantExtras) {
				t.Errorf("extras = %v, want %v", extras, tc.wantExtras)
			}
			enqueueable := len(extras) == 0
			if enqueueable != tc.wantEnqueueable {
				t.Errorf("enqueueable = %v, want %v (extras=%v)", enqueueable, tc.wantEnqueueable, extras)
			}
		})
	}
}

func TestRunPushAll_PushesWhenRemoteURLChanges(t *testing.T) {
	campDir, _ := setupPushAllCampaignWithSubmodule(t)
	ctx := context.Background()

	subDir := filepath.Join(campDir, "projects", "test-project")
	newRemote := t.TempDir()
	runPushTestCmd(t, "git", "init", "--bare", "--initial-branch=main", newRemote)
	runPushTestCmd(t, "git", "-C", subDir, "remote", "set-url", "origin", newRemote)

	if err := runPushAll(ctx, campDir, nil, false); err != nil {
		t.Fatalf("runPushAll() error = %v", err)
	}

	heads := runPushTestCmd(t, "git", "ls-remote", "--heads", newRemote, "refs/heads/main")
	if strings.TrimSpace(heads) == "" {
		t.Fatal("expected main to be pushed to the new remote")
	}
}

func setupPushAllCampaignWithSubmodule(t *testing.T) (string, string) {
	t.Helper()

	remoteDir := t.TempDir()
	runPushTestCmd(t, "git", "init", "--bare", "--initial-branch=main", remoteDir)

	seedDir := t.TempDir()
	runPushTestCmd(t, "git", "clone", remoteDir, seedDir)
	runPushTestCmd(t, "git", "-C", seedDir, "config", "user.email", "test@test.com")
	runPushTestCmd(t, "git", "-C", seedDir, "config", "user.name", "Test")
	runPushTestCmd(t, "git", "-C", seedDir, "checkout", "-B", "main")
	if err := os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("# Test Project"), 0644); err != nil {
		t.Fatalf("write seed README: %v", err)
	}
	runPushTestCmd(t, "git", "-C", seedDir, "add", ".")
	runPushTestCmd(t, "git", "-C", seedDir, "commit", "-m", "Initial commit")
	runPushTestCmd(t, "git", "-C", seedDir, "push", "origin", "main")

	campDir := t.TempDir()
	runPushTestCmd(t, "git", "init", "--initial-branch=main", campDir)
	runPushTestCmd(t, "git", "-C", campDir, "config", "user.email", "test@test.com")
	runPushTestCmd(t, "git", "-C", campDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(campDir, "README.md"), []byte("# Campaign"), 0644); err != nil {
		t.Fatalf("write campaign README: %v", err)
	}
	runPushTestCmd(t, "git", "-C", campDir, "add", ".")
	runPushTestCmd(t, "git", "-C", campDir, "commit", "-m", "Initial campaign commit")

	runPushTestCmdWithEnv(t, campDir, []string{"GIT_ALLOW_PROTOCOL=file"}, "git", "submodule", "add", remoteDir, "projects/test-project")
	runPushTestCmd(t, "git", "-C", campDir, "commit", "-m", "Add submodule")

	return campDir, remoteDir
}

func runPushTestCmd(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\nOutput: %s", name, args, err, output)
	}
	return string(output)
}

func runPushTestCmdWithEnv(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\nOutput: %s", name, args, err, output)
	}
	return string(output)
}
