//go:build container_fs

package commit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/defercommit"
)

func synchronousRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte("id: test-campaign\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return root
}

// A --json caller promised a hash. With deferral otherwise allowed, a
// synchronous commit lands inline and never reports "queued".
func TestDoCommitSynchronousNeverDefers(t *testing.T) {
	t.Setenv(defercommit.EnvNoDefer, "")
	root := synchronousRepo(t)
	if err := os.WriteFile(filepath.Join(root, "moved.md"), []byte("moved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if allowed, why := defercommit.AllowedForPaths(ctx, root, root, []string{"moved.md"}, nil); !allowed {
		t.Fatalf("fixture must allow deferral for the test to mean anything: %s", why)
	}

	res := doCommit(ctx, Options{CampaignRoot: root, CampaignID: "test-campaign", Files: []string{"moved.md"}, Synchronous: true}, "Crawl", "dungeon crawl completed", "")
	if res.Deferred || !res.Committed || res.Hash == "" || res.Err != nil {
		t.Fatalf("synchronous commit was not made inline: %+v", res)
	}
}
