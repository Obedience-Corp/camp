package jobs

import (
	"context"
	"errors"
	"testing"
)

// An empty commit-tree job is a success that runs neither the writer nor
// commit-tree. The captured tree equals its parent, so there is nothing to
// land and nothing for a message to describe.
func TestEmptyCommitTreeIsNoop(t *testing.T) {
	writerRuns := 0
	oldWrite := writeMessage
	writeMessage = func(context.Context, string, string, *Job) (string, error) {
		writerRuns++
		return "should not run", nil
	}
	t.Cleanup(func() { writeMessage = oldWrite })

	oldEmpty := isEmptyCommitTree
	isEmptyCommitTree = func(context.Context, string, *Job) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() { isEmptyCommitTree = oldEmpty })

	job := &Job{
		ID:        "job-empty",
		Kind:      KindCommitTree,
		Repo:      ".",
		Tree:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Parent:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		AutoWrite: true,
	}
	if err := executeCommitTree(context.Background(), t.TempDir(), t.TempDir(), job); err != nil {
		t.Fatalf("empty tree job error = %v, want nil", err)
	}
	if writerRuns != 0 {
		t.Fatalf("writer runs = %d, want 0 for an empty tree", writerRuns)
	}
}

// A writer failure fails the job. Camp must not invent a subject and must not
// land a commit.
func TestWriterFailureDoesNotInventMessage(t *testing.T) {
	oldEmpty := isEmptyCommitTree
	isEmptyCommitTree = func(context.Context, string, *Job) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() { isEmptyCommitTree = oldEmpty })

	boom := errors.New("daemon not running")
	oldWrite := writeMessage
	writeMessage = func(context.Context, string, string, *Job) (string, error) {
		return "", boom
	}
	t.Cleanup(func() { writeMessage = oldWrite })

	job := &Job{
		ID:        "job-writer-fail",
		Kind:      KindCommitTree,
		Repo:      ".",
		Tree:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Parent:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		AutoWrite: true,
	}
	err := executeCommitTree(context.Background(), t.TempDir(), t.TempDir(), job)
	if err == nil {
		t.Fatal("writer failure must fail the job")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

// Empty Parent is unborn HEAD, not a missing field. The execute guard must
// not reject it before the empty-tree / writer path runs; otherwise enqueue
// would accept a promise the worker immediately fails.
func TestUnbornHeadParentIsNotMalformed(t *testing.T) {
	writerRuns := 0
	oldWrite := writeMessage
	writeMessage = func(context.Context, string, string, *Job) (string, error) {
		writerRuns++
		return "should not run", nil
	}
	t.Cleanup(func() { writeMessage = oldWrite })

	oldEmpty := isEmptyCommitTree
	isEmptyCommitTree = func(context.Context, string, *Job) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() { isEmptyCommitTree = oldEmpty })

	job := &Job{
		ID:        "job-unborn",
		Kind:      KindCommitTree,
		Repo:      ".",
		Tree:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AutoWrite: true,
	}
	if err := executeCommitTree(context.Background(), t.TempDir(), t.TempDir(), job); err != nil {
		t.Fatalf("empty parent (unborn HEAD) error = %v, want nil", err)
	}
	if writerRuns != 0 {
		t.Fatalf("writer runs = %d, want 0 for the empty-tree short-circuit", writerRuns)
	}
}
