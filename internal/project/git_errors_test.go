package project

import (
	"strings"
	"testing"
)

func TestDiagnoseGitError_SSHAuthFailure(t *testing.T) {
	output := "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository."
	err := DiagnoseGitError("git submodule add", output, 128)

	if !strings.Contains(err.Diagnosis, "SSH authentication failed") {
		t.Errorf("expected SSH authentication diagnosis, got: %s", err.Diagnosis)
	}
	if !strings.Contains(err.Fix, "ssh-keygen") {
		t.Errorf("expected ssh-keygen in fix instructions, got: %s", err.Fix)
	}
	if err.DocLink == "" {
		t.Error("expected documentation link for SSH authentication")
	}
}

func TestDiagnoseGitError_RepositoryNotFound(t *testing.T) {
	output := "ERROR: Repository not found.\nfatal: Could not read from remote repository."
	err := DiagnoseGitError("git submodule add", output, 128)

	if !strings.Contains(err.Diagnosis, "Repository not found") {
		t.Errorf("expected repository not found diagnosis, got: %s", err.Diagnosis)
	}
	if !strings.Contains(err.Fix, "Verify the repository URL") {
		t.Errorf("expected URL verification in fix, got: %s", err.Fix)
	}
}

func TestDiagnoseGitError_HTTPSAuthFailure(t *testing.T) {
	output := "fatal: could not read Username for 'https://github.com': terminal prompts disabled"
	err := DiagnoseGitError("git submodule add", output, 128)

	if !strings.Contains(err.Diagnosis, "HTTPS authentication failed") {
		t.Errorf("expected HTTPS authentication diagnosis, got: %s", err.Diagnosis)
	}
	if !strings.Contains(err.Fix, "SSH URLs instead") {
		t.Errorf("expected SSH recommendation in fix, got: %s", err.Fix)
	}
}

func TestDiagnoseGitError_AlreadyExistsInIndex(t *testing.T) {
	output := "fatal: 'projects/test' already exists in the index"
	err := DiagnoseGitError("git submodule add", output, 128)

	if !strings.Contains(err.Diagnosis, "already exists") {
		t.Errorf("expected 'already exists' diagnosis, got: %s", err.Diagnosis)
	}
	if !strings.Contains(err.Fix, "git rm --cached") {
		t.Errorf("expected git rm --cached in fix, got: %s", err.Fix)
	}
}

func TestDiagnoseGitError_NotAGitRepo(t *testing.T) {
	output := "fatal: not a git repository (or any of the parent directories): .git"
	err := DiagnoseGitError("git submodule add", output, 128)

	if !strings.Contains(err.Diagnosis, "not a git repository") {
		t.Errorf("expected 'not a git repository' diagnosis, got: %s", err.Diagnosis)
	}
	if !strings.Contains(err.Fix, "camp init") {
		t.Errorf("expected camp init in fix, got: %s", err.Fix)
	}
}

func TestDiagnoseGitError_NetworkFailure(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{
			name:   "could not resolve host",
			output: "fatal: unable to access 'https://github.com/': Could not resolve host: github.com",
		},
		{
			name:   "failed to connect",
			output: "fatal: unable to access 'https://github.com/': Failed to connect to github.com",
		},
		{
			name:   "connection timed out",
			output: "fatal: unable to access 'https://github.com/': Connection timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DiagnoseGitError("git submodule add", tt.output, 128)

			if !strings.Contains(err.Diagnosis, "Network connection failed") {
				t.Errorf("expected network diagnosis, got: %s", err.Diagnosis)
			}
			if !strings.Contains(err.Fix, "internet connection") {
				t.Errorf("expected internet connection in fix, got: %s", err.Fix)
			}
		})
	}
}

func TestDiagnoseGitError_GenericFatal(t *testing.T) {
	output := "fatal: some unexpected error message"
	err := DiagnoseGitError("git submodule add", output, 128)

	if !strings.Contains(err.Diagnosis, "Git command failed") {
		t.Errorf("expected generic diagnosis, got: %s", err.Diagnosis)
	}
	if !strings.Contains(err.Fix, "git config --list") {
		t.Errorf("expected git config in fix, got: %s", err.Fix)
	}
}

func TestDiagnoseGitError_Unknown(t *testing.T) {
	output := "some non-fatal error"
	err := DiagnoseGitError("git submodule add", output, 1)

	if !strings.Contains(err.Diagnosis, "Git operation failed") {
		t.Errorf("expected generic operation failed diagnosis, got: %s", err.Diagnosis)
	}
	if !strings.Contains(err.Fix, "exit code 1") {
		t.Errorf("expected exit code in fix, got: %s", err.Fix)
	}
}

func TestGitError_Error(t *testing.T) {
	tests := []struct {
		name     string
		gitErr   *GitError
		contains []string
	}{
		{
			name: "with all fields",
			gitErr: &GitError{
				Command:   "git submodule add",
				RawOutput: "raw output",
				Diagnosis: "Test diagnosis",
				Fix:       "Test fix",
				DocLink:   "https://example.com",
			},
			contains: []string{"Test diagnosis", "Test fix", "Documentation: https://example.com"},
		},
		{
			name: "without fix",
			gitErr: &GitError{
				Diagnosis: "Test diagnosis",
				DocLink:   "https://example.com",
			},
			contains: []string{"Test diagnosis", "Documentation: https://example.com"},
		},
		{
			name: "without doclink",
			gitErr: &GitError{
				Diagnosis: "Test diagnosis",
				Fix:       "Test fix",
			},
			contains: []string{"Test diagnosis", "Test fix"},
		},
		{
			name: "diagnosis only",
			gitErr: &GitError{
				Diagnosis: "Test diagnosis",
			},
			contains: []string{"Test diagnosis"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := tt.gitErr.Error()
			for _, expected := range tt.contains {
				if !strings.Contains(errMsg, expected) {
					t.Errorf("expected error to contain %q, got: %s", expected, errMsg)
				}
			}
		})
	}
}

func TestDiagnoseGitError_PreservesRawOutput(t *testing.T) {
	output := "Permission denied (publickey)"
	cmd := "git submodule add git@github.com:org/repo.git"
	err := DiagnoseGitError(cmd, output, 128)

	if err.RawOutput != output {
		t.Errorf("expected RawOutput to be preserved: got %q, want %q", err.RawOutput, output)
	}
	if err.Command != cmd {
		t.Errorf("expected Command to be preserved: got %q, want %q", err.Command, cmd)
	}
}
