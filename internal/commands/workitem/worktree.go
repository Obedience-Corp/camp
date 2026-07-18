package workitem

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
	intworktree "github.com/Obedience-Corp/camp/internal/worktree"
)

func newWorktreeCommand() *cobra.Command {
	var (
		projectName string
		name        string
		startPoint  string
		printPath   bool
	)
	cmd := &cobra.Command{
		Use:     "worktree <selector>",
		Aliases: []string{"wt"},
		Short:   "Create a project worktree from a workitem",
		Long: `Create a git worktree for a workitem and primary-link it, so commits in
that worktree carry the workitem's WI-* tag.

This is the workitem-first counterpart to 'camp project worktree add': instead
of naming a worktree and optionally tagging a workitem, you name a workitem and
the worktree name, branch, and link are derived from it.

Project resolution:
  The target project is taken from the workitem's linked project (see
  'camp workitem link --project'). When the workitem has no project link, or
  is linked to more than one, pass --project explicitly.

Re-entry:
  If the workitem already has a primary worktree link, the existing path is
  printed and no new worktree is created.

Examples:
  # Festival workitem already linked to a project
  camp workitem worktree WI-2a7950

  # Design/explore/intent workitem: name the project
  camp workitem worktree workflow/design/camp-settings-tui --project camp

  # Override the derived worktree name
  camp workitem worktree WI-2a7950 --name grok-list-fix

  # Print only the path (for shell integration)
  cd "$(camp workitem worktree WI-2a7950 --print)"`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Creates an isolated worktree for a workitem; --print yields a cd-able path",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorktree(cmd, worktreeOptions{
				Selector:   args[0],
				Project:    projectName,
				Name:       name,
				StartPoint: startPoint,
				Print:      printPath,
			})
		},
	}
	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name (inferred from the workitem's project link if omitted)")
	cmd.Flags().StringVar(&name, "name", "", "Worktree/branch name (derived from the workitem if omitted)")
	cmd.Flags().StringVarP(&startPoint, "start-point", "s", "", "Base branch/commit for the new branch (default: current branch)")
	cmd.Flags().BoolVar(&printPath, "print", false, "Print only the worktree path")
	return cmd
}

type worktreeOptions struct {
	Selector   string
	Project    string
	Name       string
	StartPoint string
	Print      bool
}

func runWorktree(cmd *cobra.Command, opts worktreeOptions) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	wi, err := selector.Resolve(ctx, root, opts.Selector, selector.ResolveOptions{})
	if err != nil {
		return camperrors.Wrap(err, "resolve workitem "+opts.Selector)
	}

	registry, err := links.Load(ctx, root)
	if err != nil {
		return camperrors.Wrap(err, "load workitem links")
	}

	// Idempotent re-entry: the workitem already owns a worktree.
	if existing, ok := existingWorktreeLink(registry, wi); ok {
		return emitWorktree(cmd, opts.Print, existing, "", wi, true)
	}

	projectName, err := resolveWorktreeProject(ctx, root, registry, wi, opts.Project)
	if err != nil {
		return err
	}
	resolved, err := project.Resolve(ctx, root, projectName)
	if err != nil {
		var notFound *project.ProjectNotFoundError
		if errors.As(err, &notFound) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), ui.Dim("\n"+project.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}
	if err := resolved.RequireGit("git worktrees"); err != nil {
		return err
	}

	worktreeName := opts.Name
	if worktreeName == "" {
		worktreeName = deriveWorktreeName(wi)
	}
	if err := intworktree.ValidateName(worktreeName); err != nil {
		return camperrors.Wrap(err, "derived worktree name invalid; pass --name")
	}

	result, err := createWorktree(ctx, root, cfg, resolved, worktreeName, opts.StartPoint)
	if err != nil {
		return err
	}

	link, err := attachWorktreeLink(ctx, root, wi, filepath.ToSlash(result.RelativePath))
	if err != nil {
		return camperrors.Wrap(err, "worktree created but workitem link failed")
	}
	_ = link
	return emitWorktree(cmd, opts.Print, filepath.ToSlash(result.RelativePath), result.Branch, wi, false)
}

// createWorktree builds the worktree via the shared creator, mirroring
// 'camp project worktree add': a new branch named after the worktree, based on
// --start-point or the project's current branch.
func createWorktree(
	ctx context.Context,
	root string,
	cfg *config.CampaignConfig,
	resolved *project.ResolveResult,
	name, startPoint string,
) (*intworktree.CreateResult, error) {
	resolver := paths.NewResolver(root, cfg.Paths())
	creator := intworktree.NewCreator(resolver, cfg)
	opts := &intworktree.CreateOptions{
		Project:     resolved.Name,
		ProjectPath: resolved.Path,
		Name:        name,
		NewBranch:   true,
		Branch:      name,
		StartPoint:  startPoint,
	}
	if opts.StartPoint == "" {
		git := intworktree.NewGitWorktree(resolved.Path)
		current, err := git.CurrentBranch(ctx)
		if err != nil {
			return nil, camperrors.Wrap(err, "failed to detect current branch")
		}
		opts.StartPoint = current
	}

	result, err := creator.Create(ctx, opts)
	if err != nil {
		if errors.Is(err, intworktree.ErrWorktreeExists) {
			return nil, camperrors.Wrap(err,
				fmt.Sprintf("link it with 'camp workitem link %s --worktree %s/%s' or choose another --name",
					name, resolved.Name, name))
		}
		return nil, err
	}
	return result, nil
}

// resolveWorktreeProject returns the project to create the worktree in: the
// explicit flag when set, otherwise the workitem's single linked project.
func resolveWorktreeProject(ctx context.Context, root string, registry *links.Links, wi *wkitem.WorkItem, flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	projects := linkedProjects(registry, wi)
	switch len(projects) {
	case 1:
		return projects[0], nil
	case 0:
		return "", camperrors.NewValidation("project",
			fmt.Sprintf("workitem %s has no linked project; pass --project <name>", identityOf(wi)), nil)
	default:
		return "", camperrors.NewValidation("project",
			fmt.Sprintf("workitem %s is linked to multiple projects (%s); pass --project <name>",
				identityOf(wi), strings.Join(projects, ", ")), nil)
	}
}

// linkedProjects returns the distinct project names the workitem is linked to,
// derived from project-scope links (whose path is projects/<name>).
func linkedProjects(registry *links.Links, wi *wkitem.WorkItem) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, link := range registry.Links {
		if link.Scope.Kind != links.ScopeProject || !linkMatchesWorkitem(link, wi) {
			continue
		}
		name := strings.Trim(strings.TrimPrefix(filepath.ToSlash(link.Scope.Path), "projects/"), "/")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

// existingWorktreeLink returns the relative path of the primary worktree already
// linked to the workitem, if any.
func existingWorktreeLink(registry *links.Links, wi *wkitem.WorkItem) (string, bool) {
	for _, link := range registry.Links {
		if link.Role == links.RolePrimary && link.Scope.Kind == links.ScopeWorktree && linkMatchesWorkitem(link, wi) {
			return link.Scope.Path, true
		}
	}
	return "", false
}

func linkMatchesWorkitem(link links.Link, wi *wkitem.WorkItem) bool {
	if wi.StableID != "" && link.WorkitemID == wi.StableID {
		return true
	}
	return wi.Key != "" && link.WorkitemKey == wi.Key
}

// attachWorktreeLink primary-links the worktree so the resolver (and therefore
// camp p commit) picks up the workitem ref inside that tree.
func attachWorktreeLink(ctx context.Context, root string, wi *wkitem.WorkItem, relativeWorktreePath string) (links.Link, error) {
	if relativeWorktreePath == "" {
		return links.Link{}, camperrors.NewValidation("worktree", "missing worktree relative path", nil)
	}
	workitemID := wkitem.LinkWorkitemID(wi)
	return links.AttachPrimary(ctx, root, links.AttachOptions{
		WorkitemID:  workitemID,
		WorkitemKey: wi.Key,
		Scope: links.LinkScope{
			Kind: links.ScopeWorktree,
			Path: relativeWorktreePath,
		},
		CreatedBy: "camp_workitem_worktree",
		Replace:   true,
	})
}

// deriveWorktreeName builds a branch-safe worktree name from the workitem,
// preferring the last path segment of its location (already slug-like).
func deriveWorktreeName(wi *wkitem.WorkItem) string {
	candidate := ""
	if wi.RelativePath != "" {
		candidate = path.Base(filepath.ToSlash(wi.RelativePath))
	}
	if candidate == "" || candidate == "." || candidate == "/" {
		candidate = wi.Key
	}
	if candidate == "" {
		candidate = wi.Title
	}
	return sanitizeWorktreeName(candidate)
}

// sanitizeWorktreeName coerces an arbitrary string into a name accepted by
// worktree.ValidateName: lowercase alphanumerics, hyphens, and underscores,
// starting on an alphanumeric.
func sanitizeWorktreeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-_")
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-_")
	}
	return out
}

func emitWorktree(cmd *cobra.Command, printOnly bool, relPath, branch string, wi *wkitem.WorkItem, reused bool) error {
	out := cmd.OutOrStdout()
	if printOnly {
		_, err := fmt.Fprintln(out, relPath)
		return err
	}

	var lines []string
	if reused {
		lines = []string{
			ui.Success(fmt.Sprintf("Worktree already linked for workitem %s", identityOf(wi))),
			fmt.Sprintf("  Path: %s", ui.Value(relPath)),
		}
	} else {
		lines = []string{
			ui.Success("Created worktree from workitem " + identityOf(wi)),
			fmt.Sprintf("  Path:     %s", ui.Value(relPath)),
		}
		if branch != "" {
			lines = append(lines, fmt.Sprintf("  Branch:   %s", ui.Value(branch)))
		}
		lines = append(lines, ui.Dim("  camp p commit in this worktree will include WI-* in the campaign tag"))
	}
	lines = append(lines, ui.Dim(fmt.Sprintf("To navigate: cd %s", relPath)))

	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}
