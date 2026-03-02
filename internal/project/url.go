package project

import (
	"fmt"
	"net/url"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// URLType represents the type of git URL.
type URLType int

const (
	// URLTypeSSH represents SSH URLs like git@github.com:org/repo.git
	URLTypeSSH URLType = iota
	// URLTypeHTTPS represents HTTPS URLs like https://github.com/org/repo.git
	URLTypeHTTPS
	// URLTypeLocal represents local file paths
	URLTypeLocal
)

// ParsedGitURL contains information about a parsed git URL.
type ParsedGitURL struct {
	// Type is the URL type (SSH, HTTPS, or Local).
	Type URLType
	// Host is the git provider host (e.g., github.com, gitlab.com).
	Host string
	// Path is the repository path (e.g., org/repo.git).
	Path string
	// Original is the original URL string.
	Original string
}

// ParseGitURL parses and validates a git URL.
func ParseGitURL(rawURL string) (*ParsedGitURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("URL cannot be empty")
	}

	// Check for SSH URLs (git@host:path or ssh://...)
	if strings.HasPrefix(rawURL, "git@") {
		return parseSSHURL(rawURL)
	}

	if strings.HasPrefix(rawURL, "ssh://") {
		return parseSSHProtocolURL(rawURL)
	}

	// Check for HTTPS URLs
	if strings.HasPrefix(rawURL, "https://") || strings.HasPrefix(rawURL, "http://") {
		return parseHTTPSURL(rawURL)
	}

	// Check for local paths (absolute or relative)
	if strings.HasPrefix(rawURL, "/") || strings.HasPrefix(rawURL, "./") || strings.HasPrefix(rawURL, "../") {
		return &ParsedGitURL{
			Type:     URLTypeLocal,
			Host:     "",
			Path:     rawURL,
			Original: rawURL,
		}, nil
	}

	return nil, fmt.Errorf("invalid git URL format: %s\nExpected formats:\n  SSH:   git@github.com:org/repo.git\n  HTTPS: https://github.com/org/repo.git\n  Local: /path/to/repo or ./repo", rawURL)
}

// parseSSHURL parses SSH URLs in the format git@host:path
func parseSSHURL(rawURL string) (*ParsedGitURL, error) {
	// Format: git@github.com:org/repo.git
	parts := strings.SplitN(rawURL, "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid SSH URL format: %s\nExpected: git@host:org/repo.git", rawURL)
	}

	hostPath := parts[1]
	colonIdx := strings.Index(hostPath, ":")
	if colonIdx == -1 {
		return nil, fmt.Errorf("invalid SSH URL format: %s\nExpected: git@host:org/repo.git", rawURL)
	}

	host := hostPath[:colonIdx]
	path := hostPath[colonIdx+1:]

	if host == "" {
		return nil, fmt.Errorf("invalid SSH URL: host cannot be empty")
	}
	if path == "" {
		return nil, fmt.Errorf("invalid SSH URL: repository path cannot be empty")
	}

	return &ParsedGitURL{
		Type:     URLTypeSSH,
		Host:     host,
		Path:     path,
		Original: rawURL,
	}, nil
}

// parseSSHProtocolURL parses SSH URLs in the format ssh://git@host/path
func parseSSHProtocolURL(rawURL string) (*ParsedGitURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, camperrors.Wrap(err, "invalid SSH URL")
	}

	host := u.Host
	// url.Parse separates user info from host, but we want to preserve it
	if u.User != nil && u.User.Username() != "" {
		host = u.User.Username() + "@" + u.Host
	}

	if u.Host == "" {
		return nil, fmt.Errorf("invalid SSH URL: host cannot be empty")
	}
	if u.Path == "" || u.Path == "/" {
		return nil, fmt.Errorf("invalid SSH URL: repository path cannot be empty")
	}

	return &ParsedGitURL{
		Type:     URLTypeSSH,
		Host:     host,
		Path:     strings.TrimPrefix(u.Path, "/"),
		Original: rawURL,
	}, nil
}

// parseHTTPSURL parses HTTPS URLs in the format https://host/path
func parseHTTPSURL(rawURL string) (*ParsedGitURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, camperrors.Wrap(err, "invalid HTTPS URL")
	}

	if u.Host == "" {
		return nil, fmt.Errorf("invalid HTTPS URL: host cannot be empty")
	}
	if u.Path == "" || u.Path == "/" {
		return nil, fmt.Errorf("invalid HTTPS URL: repository path cannot be empty")
	}

	return &ParsedGitURL{
		Type:     URLTypeHTTPS,
		Host:     u.Host,
		Path:     strings.TrimPrefix(u.Path, "/"),
		Original: rawURL,
	}, nil
}

// String returns the original URL string.
func (p *ParsedGitURL) String() string {
	return p.Original
}
