package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// LinkOptions configures linking an existing local directory into a campaign.
type LinkOptions struct {
	Name string
	Path string
}

// LinkResult contains information about the linked project.
type LinkResult struct {
	Name   string
	Path   string
	Source string
	Type   string
	IsGit  bool
}

// AddLinked links an existing local directory into the campaign via symlink.
func AddLinked(ctx context.Context, campaignRoot, localPath string, opts LinkOptions) (*LinkResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return nil, fmt.Errorf("link path is required")
	}

	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve local path")
	}
	if resolved, err := filepath.EvalSymlinks(absLocal); err == nil {
		absLocal = resolved
	}

	info, err := os.Stat(absLocal)
	if err != nil {
		return nil, camperrors.Wrap(err, "stat local path")
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", localPath)
	}

	normalizedCampaignRoot, err := normalizeCampaignRoot(campaignRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve campaign root")
	}
	if err := ensureLinkMarkerAvailable(absLocal, normalizedCampaignRoot); err != nil {
		return nil, err
	}

	name := opts.Name
	if name == "" {
		name = filepath.Base(absLocal)
	}
	if err := ValidateProjectName(name); err != nil {
		return nil, err
	}

	destPath := opts.Path
	if destPath == "" {
		destPath = filepath.Join("projects", name)
	}
	fullPath := filepath.Join(campaignRoot, destPath)
	if err := pathutil.ValidateBoundary(campaignRoot, fullPath); err != nil {
		return nil, camperrors.Wrap(err, "project path boundary violation")
	}

	if _, err := os.Lstat(fullPath); err == nil {
		return nil, &ErrProjectExists{Name: name, Path: destPath}
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, camperrors.Wrap(err, "create parent directory")
	}
	if err := os.Symlink(absLocal, fullPath); err != nil {
		return nil, camperrors.Wrap(err, "create symlink")
	}

	isGit := isGitRepo(absLocal)
	marker := campaign.LinkMarker{
		Version:      1,
		CampaignRoot: normalizedCampaignRoot,
		ProjectName:  name,
	}
	if cfg, err := config.LoadCampaignConfig(ctx, normalizedCampaignRoot); err == nil {
		marker.CampaignID = cfg.ID
	}
	if err := campaign.WriteMarker(absLocal, marker); err != nil {
		_ = os.Remove(fullPath)
		return nil, camperrors.Wrap(err, "write .camp marker")
	}

	if err := ensureInfoExclude(ctx, campaignRoot, filepath.ToSlash(destPath)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update campaign exclude file: %v\n", err)
	}
	if isGit {
		if err := ensureGitInfoExclude(ctx, absLocal, campaign.LinkMarkerFile); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not ignore .camp in linked repo: %v\n", err)
		}
	}

	return &LinkResult{
		Name:   name,
		Path:   destPath,
		Source: absLocal,
		Type:   detectProjectType(absLocal),
		IsGit:  isGit,
	}, nil
}

// UnlinkProject removes a linked project symlink and local marker state.
func UnlinkProject(ctx context.Context, campaignRoot, name, targetPath string) error {
	projectPath := filepath.Join(campaignRoot, "projects", name)
	if err := os.Remove(projectPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	if targetPath != "" {
		normalizedCampaignRoot, rootErr := normalizeCampaignRoot(campaignRoot)
		if rootErr != nil {
			return camperrors.Wrap(rootErr, "resolve campaign root")
		}
		if marker, err := campaign.ReadMarker(targetPath); err == nil && sameCampaignRoot(marker.CampaignRoot, normalizedCampaignRoot) {
			if err := campaign.RemoveMarker(targetPath); err != nil {
				return err
			}
			if isGitRepo(targetPath) {
				if err := removeGitInfoExclude(ctx, targetPath, campaign.LinkMarkerFile); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not remove .camp ignore entry: %v\n", err)
				}
			}
		}
	}

	return removeInfoExclude(ctx, campaignRoot, filepath.ToSlash(filepath.Join("projects", name)))
}

func ensureLinkMarkerAvailable(projectDir, campaignRoot string) error {
	marker, err := campaign.ReadMarker(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrap(err, "read existing .camp marker")
	}

	if marker.CampaignRoot == "" {
		return nil
	}

	if sameCampaignRoot(marker.CampaignRoot, campaignRoot) {
		return nil
	}

	markerRoot, err := normalizeCampaignRoot(marker.CampaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "resolve existing marker campaign root")
	}
	if !campaign.IsCampaignRoot(markerRoot) {
		return nil
	}

	return fmt.Errorf("linked project is already linked to another campaign: %s", markerRoot)
}

func sameCampaignRoot(a, b string) bool {
	if a == "" || b == "" {
		return false
	}

	rootA, err := normalizeCampaignRoot(a)
	if err != nil {
		return false
	}
	rootB, err := normalizeCampaignRoot(b)
	if err != nil {
		return false
	}
	return rootA == rootB
}

func normalizeCampaignRoot(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolvedRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
		return resolvedRoot, nil
	}
	return absRoot, nil
}

func ensureInfoExclude(ctx context.Context, repoRoot, pattern string) error {
	path, err := gitInfoExcludePath(ctx, repoRoot)
	if err != nil {
		return err
	}
	return ensurePatternInFile(path, pattern)
}

func removeInfoExclude(ctx context.Context, repoRoot, pattern string) error {
	path, err := gitInfoExcludePath(ctx, repoRoot)
	if err != nil {
		return err
	}
	return removePatternFromFile(path, pattern)
}

func ensureGitInfoExclude(ctx context.Context, repoRoot, pattern string) error {
	return ensureInfoExclude(ctx, repoRoot, pattern)
}

func removeGitInfoExclude(ctx context.Context, repoRoot, pattern string) error {
	return removeInfoExclude(ctx, repoRoot, pattern)
}

func gitInfoExcludePath(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--git-path", "info/exclude")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(output))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, path)
	}
	return path, nil
}

func ensurePatternInFile(path, pattern string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}

	content := strings.TrimRight(string(data), "\n")
	if content != "" {
		content += "\n"
	}
	content += pattern + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func removePatternFromFile(path, pattern string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == pattern {
			continue
		}
		filtered = append(filtered, line)
	}

	content := strings.TrimRight(strings.Join(filtered, "\n"), "\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0644)
}
