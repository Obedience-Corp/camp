package project

import (
	"strings"
	"testing"
)

func TestParseGitURL_SSH(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectType  URLType
		expectHost  string
		expectPath  string
		expectError bool
	}{
		{
			name:       "GitHub SSH",
			url:        "git@github.com:org/repo.git",
			expectType: URLTypeSSH,
			expectHost: "github.com",
			expectPath: "org/repo.git",
		},
		{
			name:       "GitLab SSH",
			url:        "git@gitlab.com:group/subgroup/repo.git",
			expectType: URLTypeSSH,
			expectHost: "gitlab.com",
			expectPath: "group/subgroup/repo.git",
		},
		{
			name:       "Bitbucket SSH",
			url:        "git@bitbucket.org:workspace/repo.git",
			expectType: URLTypeSSH,
			expectHost: "bitbucket.org",
			expectPath: "workspace/repo.git",
		},
		{
			name:       "SSH without .git extension",
			url:        "git@github.com:org/repo",
			expectType: URLTypeSSH,
			expectHost: "github.com",
			expectPath: "org/repo",
		},
		{
			name:        "SSH missing colon",
			url:         "git@github.com/org/repo.git",
			expectError: true,
		},
		{
			name:        "SSH missing path",
			url:         "git@github.com:",
			expectError: true,
		},
		{
			name:        "SSH missing host",
			url:         "git@:org/repo.git",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseGitURL(tt.url)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if parsed.Type != tt.expectType {
				t.Errorf("Type = %v, want %v", parsed.Type, tt.expectType)
			}
			if parsed.Host != tt.expectHost {
				t.Errorf("Host = %q, want %q", parsed.Host, tt.expectHost)
			}
			if parsed.Path != tt.expectPath {
				t.Errorf("Path = %q, want %q", parsed.Path, tt.expectPath)
			}
			if parsed.Original != tt.url {
				t.Errorf("Original = %q, want %q", parsed.Original, tt.url)
			}
		})
	}
}

func TestParseGitURL_SSHProtocol(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectType  URLType
		expectHost  string
		expectPath  string
		expectError bool
	}{
		{
			name:       "SSH protocol",
			url:        "ssh://git@github.com/org/repo.git",
			expectType: URLTypeSSH,
			expectHost: "git@github.com",
			expectPath: "org/repo.git",
		},
		{
			name:        "SSH protocol missing path",
			url:         "ssh://git@github.com/",
			expectError: true,
		},
		{
			name:        "SSH protocol missing host",
			url:         "ssh:///org/repo.git",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseGitURL(tt.url)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if parsed.Type != tt.expectType {
				t.Errorf("Type = %v, want %v", parsed.Type, tt.expectType)
			}
			if parsed.Host != tt.expectHost {
				t.Errorf("Host = %q, want %q", parsed.Host, tt.expectHost)
			}
			if parsed.Path != tt.expectPath {
				t.Errorf("Path = %q, want %q", parsed.Path, tt.expectPath)
			}
		})
	}
}

func TestParseGitURL_HTTPS(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectType  URLType
		expectHost  string
		expectPath  string
		expectError bool
	}{
		{
			name:       "GitHub HTTPS",
			url:        "https://github.com/org/repo.git",
			expectType: URLTypeHTTPS,
			expectHost: "github.com",
			expectPath: "org/repo.git",
		},
		{
			name:       "GitLab HTTPS",
			url:        "https://gitlab.com/group/subgroup/repo.git",
			expectType: URLTypeHTTPS,
			expectHost: "gitlab.com",
			expectPath: "group/subgroup/repo.git",
		},
		{
			name:       "HTTP (insecure)",
			url:        "http://example.com/repo.git",
			expectType: URLTypeHTTPS,
			expectHost: "example.com",
			expectPath: "repo.git",
		},
		{
			name:       "HTTPS without .git extension",
			url:        "https://github.com/org/repo",
			expectType: URLTypeHTTPS,
			expectHost: "github.com",
			expectPath: "org/repo",
		},
		{
			name:        "HTTPS missing path",
			url:         "https://github.com/",
			expectError: true,
		},
		{
			name:        "HTTPS missing host",
			url:         "https:///org/repo.git",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseGitURL(tt.url)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if parsed.Type != tt.expectType {
				t.Errorf("Type = %v, want %v", parsed.Type, tt.expectType)
			}
			if parsed.Host != tt.expectHost {
				t.Errorf("Host = %q, want %q", parsed.Host, tt.expectHost)
			}
			if parsed.Path != tt.expectPath {
				t.Errorf("Path = %q, want %q", parsed.Path, tt.expectPath)
			}
		})
	}
}

func TestParseGitURL_Local(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		expectType URLType
		expectPath string
	}{
		{
			name:       "absolute path",
			url:        "/path/to/repo",
			expectType: URLTypeLocal,
			expectPath: "/path/to/repo",
		},
		{
			name:       "relative path current dir",
			url:        "./repo",
			expectType: URLTypeLocal,
			expectPath: "./repo",
		},
		{
			name:       "relative path parent dir",
			url:        "../repo",
			expectType: URLTypeLocal,
			expectPath: "../repo",
		},
		{
			name:       "absolute path with .git",
			url:        "/path/to/repo.git",
			expectType: URLTypeLocal,
			expectPath: "/path/to/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseGitURL(tt.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if parsed.Type != tt.expectType {
				t.Errorf("Type = %v, want %v", parsed.Type, tt.expectType)
			}
			if parsed.Path != tt.expectPath {
				t.Errorf("Path = %q, want %q", parsed.Path, tt.expectPath)
			}
			if parsed.Host != "" {
				t.Errorf("Host = %q, want empty string", parsed.Host)
			}
		})
	}
}

func TestParseGitURL_Invalid(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		expectErrText string
	}{
		{
			name:          "empty string",
			url:           "",
			expectErrText: "URL cannot be empty",
		},
		{
			name:          "whitespace only",
			url:           "   ",
			expectErrText: "URL cannot be empty",
		},
		{
			name:          "invalid format",
			url:           "not-a-valid-url",
			expectErrText: "invalid git URL format",
		},
		{
			name:          "random text",
			url:           "some random text",
			expectErrText: "invalid git URL format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseGitURL(tt.url)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.expectErrText) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.expectErrText)
			}
		})
	}
}

func TestParseGitURL_WhitespaceTrimming(t *testing.T) {
	tests := []string{
		"  git@github.com:org/repo.git  ",
		"\tgit@github.com:org/repo.git\n",
		"  https://github.com/org/repo.git  ",
	}

	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			parsed, err := ParseGitURL(url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.TrimSpace(parsed.Original) != parsed.Original {
				t.Errorf("Original URL was not trimmed: %q", parsed.Original)
			}
		})
	}
}

func TestParsedGitURL_String(t *testing.T) {
	url := "git@github.com:org/repo.git"
	parsed, err := ParseGitURL(url)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.String() != url {
		t.Errorf("String() = %q, want %q", parsed.String(), url)
	}
}
