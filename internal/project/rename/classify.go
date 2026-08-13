package rename

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/google/uuid"
)

type declaredSubmodule struct {
	Section string
	Path    string
	URL     string
}

// Plan resolves one exact managed project and validates the full destination
// before any filesystem or Git mutation is attempted.
func Plan(ctx context.Context, campaignRoot, current, newName string, opts Options) (*PlanResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := projectsvc.ValidateProjectName(newName); err != nil {
		return nil, err
	}
	if opts.RemoteURL != "" {
		if _, err := projectsvc.ParseGitURL(opts.RemoteURL); err != nil {
			return nil, camperrors.NewValidation("remote-url", "must be a valid Git URL or repository path", err)
		}
	}
	if strings.Contains(current, "@") {
		return nil, camperrors.NewValidation("current", "virtual monorepo projects cannot be renamed", nil)
	}

	campaignRoot, err := filepath.Abs(campaignRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve campaign root")
	}
	projectsRel, err := configuredProjectsRoot(ctx, campaignRoot)
	if err != nil {
		return nil, err
	}
	oldName, oldRel, err := normalizeCurrent(current, projectsRel)
	if err != nil {
		return nil, err
	}
	if oldName == newName {
		return nil, camperrors.NewValidation("new", "new project name must differ from current name", nil)
	}

	newRel := filepath.ToSlash(filepath.Join(projectsRel, newName))
	oldAbs := filepath.Join(campaignRoot, filepath.FromSlash(oldRel))
	newAbs := filepath.Join(campaignRoot, filepath.FromSlash(newRel))
	if _, err := os.Lstat(newAbs); err == nil {
		return nil, camperrors.Wrapf(camperrors.ErrAlreadyExists, "project destination already exists: %s", newRel)
	} else if !os.IsNotExist(err) {
		return nil, camperrors.Wrap(err, "inspect project destination")
	}

	declared, err := declaredSubmodules(ctx, campaignRoot)
	if err != nil {
		return nil, err
	}
	entry, isDeclared := declared[oldRel]
	if existing, ok := declared[newRel]; ok && (!isDeclared || existing.Section != entry.Section) {
		return nil, camperrors.Wrapf(camperrors.ErrAlreadyExists, "submodule destination is already declared: %s", newRel)
	}
	info, statErr := os.Lstat(oldAbs)
	if statErr != nil && (!isDeclared || !os.IsNotExist(statErr)) {
		if os.IsNotExist(statErr) {
			return nil, camperrors.NewNotFound("project", current, statErr)
		}
		return nil, camperrors.Wrap(statErr, "inspect project source")
	}

	plan := &PlanResult{
		OperationID: uuid.NewString(),
		OldName:     oldName, NewName: newName,
		OldPath: oldRel, NewPath: newRel,
		NewURL:             opts.RemoteURL,
		CampaignRoot:       campaignRoot,
		ProjectsRoot:       projectsRel,
		CommitFiles:        []string{oldRel, newRel},
		AutoCommitEligible: true,
	}

	switch {
	case isDeclared:
		plan.Kind = KindSubmodule
		plan.SubmoduleSection = entry.Section
		plan.OldURL = entry.URL
		plan.CommitFiles = append(plan.CommitFiles, ".gitmodules")
		if _, err := os.Stat(filepath.Join(oldAbs, ".git")); err == nil {
			plan.Initialized = true
			if err := validateSubmoduleGitDir(ctx, campaignRoot, oldAbs, oldRel, newRel); err != nil {
				return nil, err
			}
			plan.Worktrees, err = inventoryWorktrees(ctx, campaignRoot, projectsRel, oldName, newName, oldAbs)
			if err != nil {
				return nil, err
			}
		}
	case info != nil && info.Mode()&os.ModeSymlink != 0:
		plan.Kind = KindLinked
		plan.LinkedTarget, err = filepath.EvalSymlinks(oldAbs)
		if err != nil {
			return nil, camperrors.Wrap(err, "resolve linked project")
		}
		if _, err := os.Stat(filepath.Join(plan.LinkedTarget, ".git")); err == nil {
			plan.Initialized = true
			plan.Worktrees, err = inventoryWorktrees(ctx, campaignRoot, projectsRel, oldName, newName, plan.LinkedTarget)
			if err != nil {
				return nil, err
			}
		} else if opts.RemoteURL != "" {
			return nil, camperrors.NewValidation("remote-url", "a non-Git linked project has no remote", nil)
		}
	case info != nil && info.IsDir():
		tracked, err := trackedBelow(ctx, campaignRoot, oldRel)
		if err != nil {
			return nil, err
		}
		if !tracked {
			return nil, camperrors.NewValidation("current", "directory is not owned by the campaign Git index; use camp project add or camp project link first", nil)
		}
		if _, err := os.Lstat(filepath.Join(oldAbs, ".git")); err == nil {
			return nil, camperrors.NewValidation("current", "undeclared nested Git repositories cannot be renamed; use camp project add or camp project link first", nil)
		}
		if opts.RemoteURL != "" {
			return nil, camperrors.NewValidation("remote-url", "campaign-owned directories have no separate remote", nil)
		}
		plan.Kind = KindCampaignDir
	default:
		return nil, camperrors.NewValidation("current", "source is not a supported managed project", nil)
	}

	if opts.RemoteURL == "" {
		plan.NewURL = plan.OldURL
	}
	plan.Metadata, plan.CommitFiles, err = planMetadata(campaignRoot, oldName, newName, oldRel, newRel, opts.RemoteURL, plan.CommitFiles)
	if err != nil {
		return nil, err
	}
	commitInspection := append([]string(nil), plan.CommitFiles...)
	if plan.Kind != KindCampaignDir {
		commitInspection = removeStrings(commitInspection, oldRel, newRel)
	}
	if len(commitInspection) > 0 {
		status, statusErr := git.RunGitCmd(ctx, campaignRoot, append([]string{"status", "--porcelain", "--"}, commitInspection...)...)
		if statusErr != nil {
			return nil, statusErr
		}
		if strings.TrimSpace(status) != "" {
			plan.AutoCommitEligible = false
			plan.AutoCommitSkipReason = "affected tracked files already contain changes; automatic commit would mix them with the rename"
		}
	}
	return plan, nil
}

func configuredProjectsRoot(ctx context.Context, root string) (string, error) {
	rel := "projects"
	if jumps, err := config.LoadJumpsConfig(ctx, root); err != nil {
		return "", err
	} else if jumps != nil && jumps.Paths.Projects != "" {
		rel = jumps.Paths.Projects
	}
	rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", camperrors.NewValidation("projects path", "must be a campaign-relative directory", nil)
	}
	return strings.TrimSuffix(rel, "/"), nil
}

func normalizeCurrent(current, projectsRel string) (string, string, error) {
	current = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(current))))
	if current == "." || filepath.IsAbs(current) || current == ".." || strings.HasPrefix(current, "../") {
		return "", "", camperrors.NewValidation("current", "must be a project name or exact managed path", nil)
	}
	name := current
	if strings.Contains(current, "/") {
		prefix := strings.TrimSuffix(projectsRel, "/") + "/"
		if !strings.HasPrefix(current, prefix) || strings.Contains(strings.TrimPrefix(current, prefix), "/") {
			return "", "", camperrors.NewValidation("current", "must identify one top-level managed project", nil)
		}
		name = strings.TrimPrefix(current, prefix)
	}
	if err := projectsvc.ValidateProjectName(name); err != nil {
		return "", "", err
	}
	return name, filepath.ToSlash(filepath.Join(projectsRel, name)), nil
}

func declaredSubmodules(ctx context.Context, root string) (map[string]declaredSubmodule, error) {
	result := make(map[string]declaredSubmodule)
	path := filepath.Join(root, ".gitmodules")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return result, nil
	}
	out, err := git.RunGitCmd(ctx, root, "config", "-f", ".gitmodules", "--get-regexp", `^submodule\..*\.path$`)
	if err != nil {
		return nil, camperrors.Wrap(err, "parse .gitmodules")
	}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := fields[0]
		section := strings.TrimSuffix(strings.TrimPrefix(key, "submodule."), ".path")
		subPath := filepath.ToSlash(filepath.Clean(strings.Join(fields[1:], " ")))
		url, _ := git.RunGitCmd(ctx, root, "config", "-f", ".gitmodules", "--get", "submodule."+section+".url")
		if _, exists := result[subPath]; exists {
			return nil, camperrors.NewValidation(".gitmodules", fmt.Sprintf("multiple sections declare path %q", subPath), nil)
		}
		result[subPath] = declaredSubmodule{Section: section, Path: subPath, URL: strings.TrimSpace(url)}
	}
	return result, scanner.Err()
}

func trackedBelow(ctx context.Context, root, rel string) (bool, error) {
	out, err := git.RunGitCmd(ctx, root, "ls-files", "--", rel)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func inventoryWorktrees(ctx context.Context, root, projectsRel, oldName, newName, repo string) ([]WorktreeChange, error) {
	entries, err := worktree.NewGitWorktree(repo).List(ctx)
	if err != nil {
		return nil, err
	}
	rootGitDir, _ := git.RunGitCmd(ctx, root, "rev-parse", "--absolute-git-dir")
	rootGitDir = filepath.Clean(strings.TrimSpace(rootGitDir))
	oldRoot := filepath.Join(root, filepath.FromSlash(projectsRel), "worktrees", oldName)
	newRoot := filepath.Join(root, filepath.FromSlash(projectsRel), "worktrees", newName)
	mainPath, _ := filepath.EvalSymlinks(repo)
	var changes []WorktreeChange
	for _, entry := range entries {
		path, _ := filepath.Abs(entry.Path)
		resolved, _ := filepath.EvalSymlinks(path)
		insideRootGitDir := rootGitDir != "" && (path == rootGitDir || strings.HasPrefix(path, rootGitDir+string(filepath.Separator)))
		if entry.IsBare || insideRootGitDir || resolved == mainPath || path == repo {
			continue
		}
		change := WorktreeChange{Before: path, After: path, External: true}
		if rel, err := filepath.Rel(oldRoot, path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			change.After = filepath.Join(newRoot, rel)
			change.Moved = true
			change.External = false
			if _, err := os.Lstat(change.After); err == nil {
				return nil, camperrors.Wrapf(camperrors.ErrAlreadyExists, "worktree destination already exists: %s", change.After)
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
		status, _ := git.RunGitCmd(ctx, path, "status", "--porcelain")
		change.Dirty = strings.TrimSpace(status) != ""
		changes = append(changes, change)
	}
	return changes, nil
}

func validateSubmoduleGitDir(ctx context.Context, campaignRoot, checkout, oldRel, newRel string) error {
	actual, err := git.ResolveGitDir(checkout)
	if err != nil {
		return err
	}
	rootGitDir, err := git.RunGitCmd(ctx, campaignRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	rootGitDir = strings.TrimSpace(rootGitDir)
	expected := filepath.Join(rootGitDir, "modules", filepath.FromSlash(oldRel))
	if filepath.Clean(actual) != filepath.Clean(expected) {
		return camperrors.NewValidation("submodule gitdir", "non-standard submodule Git directory cannot be moved safely", nil)
	}
	destination := filepath.Join(rootGitDir, "modules", filepath.FromSlash(newRel))
	if _, err := os.Stat(destination); err == nil {
		return camperrors.Wrapf(camperrors.ErrAlreadyExists, "submodule Git directory destination exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeStrings(values []string, removes ...string) []string {
	removed := make(map[string]bool, len(removes))
	for _, value := range removes {
		removed[filepath.ToSlash(value)] = true
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !removed[filepath.ToSlash(value)] {
			out = append(out, value)
		}
	}
	return out
}
