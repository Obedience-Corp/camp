package project

import (
	"errors"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestResolveResultRequireGit_RejectsLinkedNonGit(t *testing.T) {
	result := &ResolveResult{
		Name:   "plain-linked-dir",
		Source: SourceLinkedNonGit,
	}

	err := result.RequireGit("git commits")
	if err == nil {
		t.Fatal("RequireGit() = nil, want error")
	}

	if got, want := err.Error(), `project "plain-linked-dir" is a linked non-git directory and does not support git commits`; got != want {
		t.Fatalf("RequireGit() error = %q, want %q", got, want)
	}

	var nonGitErr *NonGitOperationError
	if !errors.As(err, &nonGitErr) {
		t.Fatalf("RequireGit() error type = %T, want *NonGitOperationError", err)
	}

	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Fatalf("RequireGit() should match ErrInvalidInput")
	}
}

func TestResolveResultRequireGit_AllowsGitProjects(t *testing.T) {
	result := &ResolveResult{
		Name:   "git-project",
		Source: SourceLinked,
	}

	if err := result.RequireGit("git commits"); err != nil {
		t.Fatalf("RequireGit() error = %v, want nil", err)
	}
}
