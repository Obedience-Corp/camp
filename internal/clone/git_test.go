package clone

import (
	"context"
	"testing"
)

func TestParseSubmoduleStatus(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantSuccess bool
		wantCommit  string
		wantPath    string
		wantError   bool
	}{
		{
			name:        "initialized submodule",
			line:        " abc123def456789 projects/sub (v1.0.0)",
			wantSuccess: true,
			wantCommit:  "abc123def456789",
			wantPath:    "projects/sub",
			wantError:   false,
		},
		{
			name:        "uninitialized submodule",
			line:        "-abc123def456789 projects/sub",
			wantSuccess: false,
			wantCommit:  "abc123def456789",
			wantPath:    "projects/sub",
			wantError:   true,
		},
		{
			name:        "commit mismatch (prefix +)",
			line:        "+abc123def456789 projects/sub (heads/main)",
			wantSuccess: true,
			wantCommit:  "abc123def456789",
			wantPath:    "projects/sub",
			wantError:   false,
		},
		{
			name:        "no prefix",
			line:        "abc123def456789 projects/sub",
			wantSuccess: true,
			wantCommit:  "abc123def456789",
			wantPath:    "projects/sub",
			wantError:   false,
		},
		{
			name:        "empty line",
			line:        "",
			wantSuccess: false,
			wantCommit:  "",
			wantPath:    "",
			wantError:   false,
		},
		{
			name:        "nested submodule path",
			line:        " fedcba987654321 projects/nested/deep/sub (v2.0.0-rc1)",
			wantSuccess: true,
			wantCommit:  "fedcba987654321",
			wantPath:    "projects/nested/deep/sub",
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSubmoduleStatus(tt.line)

			if result.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v", result.Success, tt.wantSuccess)
			}
			if result.Commit != tt.wantCommit {
				t.Errorf("Commit = %q, want %q", result.Commit, tt.wantCommit)
			}
			if result.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", result.Path, tt.wantPath)
			}
			if tt.wantError && result.Error == nil {
				t.Error("Error = nil, want error")
			}
			if !tt.wantError && result.Error != nil {
				t.Errorf("Error = %v, want nil", result.Error)
			}
		})
	}
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "https URL with .git",
			url:      "https://github.com/org/repo.git",
			expected: "repo",
		},
		{
			name:     "https URL without .git",
			url:      "https://github.com/org/repo",
			expected: "repo",
		},
		{
			name:     "ssh URL with colon",
			url:      "git@github.com:org/repo.git",
			expected: "repo",
		},
		{
			name:     "ssh:// URL",
			url:      "ssh://git@github.com/org/repo.git",
			expected: "repo",
		},
		{
			name:     "trailing slash",
			url:      "https://github.com/org/repo/",
			expected: "repo",
		},
		{
			name:     "trailing slash with .git",
			url:      "https://github.com/org/repo.git/",
			expected: "repo",
		},
		{
			name:     "just repo name",
			url:      "repo.git",
			expected: "repo",
		},
		{
			name:     "simple path",
			url:      "/path/to/repo.git",
			expected: "repo",
		},
		{
			name:     "gitlab style",
			url:      "https://gitlab.com/group/subgroup/repo.git",
			expected: "repo",
		},
		{
			name:     "bitbucket style",
			url:      "git@bitbucket.org:team/repo.git",
			expected: "repo",
		},
		{
			name:     "repo name with dots",
			url:      "https://github.com/org/my.dotted.repo.git",
			expected: "my.dotted.repo",
		},
		{
			name:     "repo name with hyphens",
			url:      "https://github.com/org/my-hyphenated-repo.git",
			expected: "my-hyphenated-repo",
		},
		{
			name:     "empty string",
			url:      "",
			expected: ".", // filepath.Base("") returns "."
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRepoName(tt.url)
			if result != tt.expected {
				t.Errorf("extractRepoName(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}

// Integration tests that require actual git operations

func TestGitClone_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner(WithURL("https://github.com/test/repo.git"))

	_, err := c.gitClone(ctx)
	if err != context.Canceled {
		t.Errorf("gitClone() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleSync_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()

	err := c.gitSubmoduleSync(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("gitSubmoduleSync() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleUpdate_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()

	err := c.gitSubmoduleUpdate(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("gitSubmoduleUpdate() error = %v, want context.Canceled", err)
	}
}

func TestGitGetBranch_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()

	_, err := c.gitGetBranch(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("gitGetBranch() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleStatus_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()

	_, err := c.gitSubmoduleStatus(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("gitSubmoduleStatus() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleURL_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()
	_, err := c.gitSubmoduleURL(ctx, "/tmp/fake", "sub")
	if err != context.Canceled {
		t.Errorf("gitSubmoduleURL() error = %v, want context.Canceled", err)
	}
}

func TestGitSubmoduleSync_InvalidDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	c := NewCloner()

	err := c.gitSubmoduleSync(ctx, "/nonexistent/path")
	if err == nil {
		t.Error("gitSubmoduleSync() error = nil, want error for invalid directory")
	}
}

func TestGitSubmoduleUpdate_InvalidDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	c := NewCloner()

	err := c.gitSubmoduleUpdate(ctx, "/nonexistent/path")
	if err == nil {
		t.Error("gitSubmoduleUpdate() error = nil, want error for invalid directory")
	}
}

// Test helpers
