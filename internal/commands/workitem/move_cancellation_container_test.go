//go:build container_fs

package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
	"github.com/Obedience-Corp/camp/internal/moveref"
)

// Both move paths rename a directory and then repair the references that
// pointed at it. These pin the boundary: once the rename has landed, the repair
// runs to completion whatever the caller does.
//
// The cancel is injected through the repair step itself rather than raced
// against the filesystem. A poller that cancels when the source directory
// disappears passes or fails on scheduling, which is how the equivalent
// assertion in shelve_bookkeeping_test.go came to fail only under load.

// railMoveFixture lays down a campaign with one directory to move and returns
// the campaign root, the source, and the destination.
func railMoveFixture(t *testing.T) (root, src, dst string) {
	t.Helper()
	root = t.TempDir()
	src = filepath.Join(root, "workflow", "design", "feat")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("# feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst = filepath.Join(root, "festivals", "ready", "feat")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, src, dst
}

// applyWorkitemMove is the shared body of the rail promote and the demote, so
// this covers both commands' cancellation semantics.
func TestApplyWorkitemMove_RepairsReferencesAfterTheCallerCancels(t *testing.T) {
	root, src, dst := railMoveFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		sawCancelledCaller bool
		repairCtxErr       error
		hadDeadline        bool
	)
	rewrite := func(rctx context.Context, root, srcPath, dstPath string) ([]string, error) {
		// The rename has landed by the time this runs. Cancel the caller here,
		// which is the exact instant the contract is about.
		cancel()
		<-ctx.Done()
		sawCancelledCaller = true

		repairCtxErr = rctx.Err()
		_, hadDeadline = rctx.Deadline()

		// Do the real work on the context the production code chose, so a
		// context that could not carry it would fail here rather than pass.
		return moveref.RewriteForMove(rctx, root, srcPath, dstPath)
	}

	var result workitemPromoteResult
	ci, err := applyWorkitemMoveWith(ctx, root, workitemMove{
		SourcePath: src,
		DestPath:   dst,
		OldRel:     "workflow/design/feat",
		NewRel:     "festivals/ready/feat",
		OldKey:     "design:workflow/design/feat",
		NewKey:     "design:festivals/ready/feat",
	}, &result, rewrite)
	if err != nil {
		t.Fatalf("move abandoned its reference repair after cancellation: %v", err)
	}
	if !sawCancelledCaller {
		t.Fatal("the repair step never ran, so the test asserted nothing")
	}
	if repairCtxErr != nil {
		t.Fatalf("repair context reports %v; it must outlive the caller's cancellation", repairCtxErr)
	}
	if !hadDeadline {
		t.Fatal("repair context has no deadline; an unbounded one cannot be interrupted at all")
	}
	if ci == nil {
		t.Fatal("commitInputs is nil, so the tail has nothing to record the move with")
	}
	if _, statErr := os.Stat(dst); statErr != nil {
		t.Fatalf("destination missing after the move: %v", statErr)
	}
	if _, statErr := os.Stat(src); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("source still present after the move: %v", statErr)
	}
}

// The entry guard is the other half of the contract: cancelling before anything
// has moved must still decline the work.
func TestApplyWorkitemMove_DeclinesWhenCancelledBeforeTheMove(t *testing.T) {
	root, src, dst := railMoveFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	rewrite := func(context.Context, string, string, string) ([]string, error) {
		called = true
		return nil, nil
	}

	var result workitemPromoteResult
	if _, err := applyWorkitemMoveWith(ctx, root, workitemMove{
		SourcePath: src, DestPath: dst,
		OldRel: "workflow/design/feat", NewRel: "festivals/ready/feat",
	}, &result, rewrite); err == nil {
		t.Fatal("a move cancelled before it began must be declined, not performed")
	}
	if called {
		t.Fatal("the repair ran after a declined move")
	}
	if _, statErr := os.Stat(src); statErr != nil {
		t.Fatalf("source was moved despite the cancellation: %v", statErr)
	}
}

// The dungeon service is the other move path, reached by camp workitem promote
// through MoveToDungeon.
func TestDungeonApplyMove_RepairsReferencesAfterTheCallerCancels(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "workflow", "design", "feat")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("# feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dungeonPath := filepath.Join(root, "dungeon")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		ran          bool
		repairCtxErr error
		hadDeadline  bool
	)
	svc := intdungeon.NewService(root, dungeonPath,
		intdungeon.WithPostMoveRewrite(func(rctx context.Context, srcPath, dstPath string) error {
			cancel()
			<-ctx.Done()
			ran = true
			repairCtxErr = rctx.Err()
			_, hadDeadline = rctx.Deadline()
			return nil
		}))
	if _, err := svc.Init(ctx, intdungeon.InitOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.MoveToDungeonStatus(ctx, "feat", filepath.Dir(src), "completed"); err != nil {
		t.Fatalf("dungeon move abandoned its reference repair after cancellation: %v", err)
	}
	if !ran {
		t.Fatal("the repair step never ran, so the test asserted nothing")
	}
	if repairCtxErr != nil {
		t.Fatalf("repair context reports %v; it must outlive the caller's cancellation", repairCtxErr)
	}
	if !hadDeadline {
		t.Fatal("repair context has no deadline; an unbounded one cannot be interrupted at all")
	}
	if _, statErr := os.Stat(src); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("source still present after the move: %v", statErr)
	}
}
