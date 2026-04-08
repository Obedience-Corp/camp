package project

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// LinkOptions configures how a local project is linked into the campaign.
type LinkOptions struct {
	// Name overrides the project name (defaults to directory basename).
	Name string
	// Path overrides the destination path (defaults to projects/<name>).
	Path string
}

// LinkResult contains information about the linked project.
type LinkResult struct {
	// Name is the project name.
	Name string
	// Path is the relative path from campaign root (symlink location).
	Path string
	// Source is the original absolute path.
	Source string
	// Type is the detected project type.
	Type string
	// IsGit indicates whether the linked project is a git repository.
	IsGit bool
}

// AddLinked links an external directory into the campaign via symlink.
// The project can be a git repo or a plain directory. The symlink is created
// inside projects/ and the project is recorded in the linked projects manifest.
// A .gitignore entry is added so git does not track the symlink.
func AddLinked(ctx context.Context, campaignRoot, localPath string, opts LinkOptions) (*LinkResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Resolve to absolute path
	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to resolve local path")
	}

	// Verify the path exists and is a directory
	info, err := os.Stat(absLocal)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("path does not exist: %s", localPath)
		}
		return nil, camperrors.Wrapf(err, "cannot access %s", localPath)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", localPath)
	}

	// Determine project name
	name := opts.Name
	if name == "" {
		name = filepath.Base(absLocal)
	}
	if err := ValidateProjectName(name); err != nil {
		return nil, err
	}

	// Determine destination path
	destPath := opts.Path
	if destPath == "" {
		destPath = filepath.Join("projects", name)
	}

	fullPath := filepath.Join(campaignRoot, destPath)

	// Check if already exists
	if _, err := os.Lstat(fullPath); err == nil {
		return nil, &ErrProjectExists{Name: name, Path: destPath}
	}

	// Check if it's a git repo
	isGit := isGitRepo(absLocal)

	// Determine source type
	source := SourceLinked
	if !isGit {
		source = SourceLinkedNonGit
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, camperrors.Wrapf(err, "create parent directory")
	}

	// Create the symlink (absolute path for reliability)
	if err := os.Symlink(absLocal, fullPath); err != nil {
		return nil, camperrors.Wrapf(err, "create symlink")
	}

	// Add to manifest
	entry := LinkedProjectEntry{
		Source:  source,
		AbsPath: absLocal,
		IsGit:  isGit,
		AddedAt: time.Now(),
	}
	if err := AddToManifest(campaignRoot, name, entry); err != nil {
		// Clean up symlink on manifest failure
		os.Remove(fullPath)
		return nil, camperrors.Wrapf(err, "record linked project")
	}

	// Add gitignore entry so the campaign repo doesn't track the symlink
	if err := addGitignoreEntry(campaignRoot, destPath); err != nil {
		// Non-fatal - warn but continue
		fmt.Fprintf(os.Stderr, "Warning: could not add .gitignore entry: %v\n", err)
	}

	// Detect project type
	projectType := detectProjectType(absLocal)

	return &LinkResult{
		Name:   name,
		Path:   destPath,
		Source: absLocal,
		Type:   projectType,
		IsGit:  isGit,
	}, nil
}

// UnlinkProject removes a linked project's symlink and manifest entry.
// Returns true if the project was a linked project that was unlinked.
func UnlinkProject(ctx context.Context, campaignRoot, name string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	isLinked, _, err := IsLinkedProject(campaignRoot, name)
	if err != nil {
		return false, err
	}
	if !isLinked {
		return false, nil
	}

	projectPath := filepath.Join(campaignRoot, "projects", name)

	// Verify it's actually a symlink before removing
	info, err := os.Lstat(projectPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Symlink already gone, just clean up manifest
			RemoveFromManifest(campaignRoot, name)
			return true, nil
		}
		return false, camperrors.Wrapf(err, "check linked project")
	}

	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(projectPath); err != nil {
			return false, camperrors.Wrapf(err, "remove symlink")
		}
	}

	// Remove from manifest
	if _, err := RemoveFromManifest(campaignRoot, name); err != nil {
		return false, err
	}

	// Remove gitignore entry
	destPath := filepath.Join("projects", name)
	if err := removeGitignoreEntry(campaignRoot, destPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not remove .gitignore entry: %v\n", err)
	}

	return true, nil
}

// isGitRepo checks whether a directory contains a .git directory or file.
func isGitRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// addGitignoreEntry appends a path to the campaign root's .gitignore
// if it's not already present.
func addGitignoreEntry(campaignRoot, entry string) error {
	gitignorePath := filepath.Join(campaignRoot, ".gitignore")

	// Normalize to forward slashes for gitignore compatibility
	entry = filepath.ToSlash(entry)

	// Check if entry already exists
	existing, err := os.ReadFile(gitignorePath)
	if err == nil {
		for _, line := range strings.Split(string(existing), "\n") {
			if strings.TrimSpace(line) == entry {
				return nil // Already present
			}
		}
	}

	// Append entry
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Add a newline before if the file doesn't end with one
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}

	// Add comment on first linked project entry
	if !strings.Contains(string(existing), "# Linked projects") {
		if _, err := f.WriteString("\n# Linked projects (machine-local symlinks)\n"); err != nil {
			return err
		}
	}

	_, err = f.WriteString(entry + "\n")
	return err
}

// removeGitignoreEntry removes a path from the campaign root's .gitignore.
func removeGitignoreEntry(campaignRoot, entry string) error {
	gitignorePath := filepath.Join(campaignRoot, ".gitignore")

	entry = filepath.ToSlash(entry)

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != entry {
			lines = append(lines, line)
		}
	}

	// Clean up empty "Linked projects" comment section if no linked entries remain
	result := strings.Join(lines, "\n")
	if !strings.Contains(result, "projects/") {
		result = strings.Replace(result, "\n# Linked projects (machine-local symlinks)\n", "", 1)
	}

	return os.WriteFile(gitignorePath, []byte(result), 0644)
}
