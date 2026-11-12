package approvalkit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/git"
)

func TestCommitPreconditions(t *testing.T) {
	ctx := context.Background()
	called := 0
	orig := scopedCommit
	scopedCommit = func(context.Context, string, []string, *git.CommitOptions) error {
		called++
		return nil
	}
	t.Cleanup(func() { scopedCommit = orig })

	if res, err := Commit(ctx, ""); err != nil || res.Status != CommitUnchanged || res.Reason == "" {
		t.Fatalf("no root: %+v, %v", res, err)
	}

	root := t.TempDir()
	if res, err := Commit(ctx, root); err != nil || res.Status != CommitUnchanged || !strings.Contains(res.Reason, "no approvals") {
		t.Fatalf("no ledger: %+v, %v", res, err)
	}

	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(Dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	// A camp that is not a git repository has nothing to commit into, and that
	// is not an error.
	if res, err := Commit(ctx, root); err != nil || res.Status != CommitUnchanged || !strings.Contains(res.Reason, "not a git repository") {
		t.Fatalf("not a repo: %+v, %v", res, err)
	}
	if called != 0 {
		t.Fatalf("the committer ran %d times before any precondition held", called)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Commit(cancelled, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: %v", err)
	}
}

func TestCommitMessage(t *testing.T) {
	msg := CommitMessage(context.Background(), t.TempDir())
	if !strings.Contains(msg, "approvals: record approval requests and decisions") {
		t.Fatalf("message = %q", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Fatalf("message must be a single subject line: %q", msg)
	}
}

func TestUncommittedOutsideARepo(t *testing.T) {
	if n := Uncommitted(context.Background(), t.TempDir()); n != 0 {
		t.Fatalf("Uncommitted in a non-repo = %d", n)
	}
	if n := Uncommitted(context.Background(), ""); n != 0 {
		t.Fatalf("Uncommitted with no root = %d", n)
	}
}
