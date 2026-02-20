package clone

import (
	"context"
	"os/exec"
	"strings"
)

// SubmoduleInfo contains parsed submodule information from .gitmodules.
type SubmoduleInfo struct {
	// Name is the submodule name (typically matches path).
	Name string
	// Path is the filesystem path relative to repo root.
	Path string
	// URL is the declared remote URL.
	URL string
}

// parseGitmodules reads and parses the .gitmodules file.
func parseGitmodules(ctx context.Context, repoPath string) ([]SubmoduleInfo, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "config", "-f", ".gitmodules", "--list")
	output, err := cmd.Output()
	if err != nil {
		// No .gitmodules file means no submodules
		return nil, nil
	}

	// Parse output: submodule.<name>.path=<path>, submodule.<name>.url=<url>
	submodules := make(map[string]*SubmoduleInfo)
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		if !strings.HasPrefix(line, "submodule.") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		// Extract submodule name from key like "submodule.<name>.path"
		// Names can contain dots (e.g., "obediencecorp.com"), so we strip
		// the "submodule." prefix and use the last "." to split name from property.
		rest := strings.TrimPrefix(key, "submodule.")
		lastDot := strings.LastIndex(rest, ".")
		if lastDot <= 0 {
			continue
		}
		name := rest[:lastDot]
		prop := rest[lastDot+1:]

		if submodules[name] == nil {
			submodules[name] = &SubmoduleInfo{Name: name}
		}

		switch prop {
		case "path":
			submodules[name].Path = value
		case "url":
			submodules[name].URL = value
		}
	}

	result := make([]SubmoduleInfo, 0, len(submodules))
	for _, sub := range submodules {
		result = append(result, *sub)
	}
	return result, nil
}
