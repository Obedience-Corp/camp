package worktrees

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/project"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
	"github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	createBranch     string
	createStartPoint string
	createTrack      string
	createWorkitem   string
)

var worktreesCreateCmd = &cobra.Command{
	Use:   "create <project> <name>",
	Short: "Create a new worktree for a project",
	Long: `Create a new git worktree for a project in the standardized location.

The worktree will be created at: projects/worktrees/<project>/<name>/

By default, creates a new branch with the worktree name based on the current branch.
Use --branch to checkout an existing branch instead.

Examples:
  # Create worktree with new branch based on current branch (default)
  camp worktrees create my-api feature-auth

  # Create worktree with new branch based on main
  camp worktrees create my-api experiment --start-point main

  # Checkout existing branch (instead of creating new)
  camp worktrees create my-api hotfix --branch hotfix-123

  # Create worktree tracking remote branch
  camp worktrees create web pr-review --track origin/feature-xyz

  # Link a design workitem so camp p commit tags WI-*
  camp worktrees create fest fest-list-watch --workitem WI-2a7950`,
	Args: cobra.ExactArgs(2),
	RunE: runWorktreesCreate,
}

func init() {
	Cmd.AddCommand(worktreesCreateCmd)

	worktreesCreateCmd.Flags().StringVarP(&createBranch, "branch", "b", "",
		"Checkout existing branch instead of creating new one")
	worktreesCreateCmd.Flags().StringVarP(&createStartPoint, "start-point", "s", "",
		"Base branch/commit for new branch (default: current branch)")
	worktreesCreateCmd.Flags().StringVarP(&createTrack, "track", "t", "",
		"Remote branch to track (creates new local tracking branch)")
	worktreesCreateCmd.Flags().StringVar(&createWorkitem, "workitem", "",
		"workitem selector (ref, path, or id) to primary-link to this worktree for camp p commit tags")
}

func runWorktreesCreate(cmd *cobra.Command, args []string) error {
	projectName := args[0]
	worktreeName := args[1]

	ctx := cmd.Context()

	// Find campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Load campaign config
	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "failed to load campaign config")
	}

	// Create resolver and creator
	resolver := paths.NewResolver(campRoot, cfg.Paths())
	creator := worktree.NewCreator(resolver, cfg)

	resolved, err := project.Resolve(ctx, campRoot, projectName)
	if err != nil {
		return err
	}
	if err := resolved.RequireGit("git worktrees"); err != nil {
		return err
	}

	// Resolve and validate the workitem before creating anything, so a bad or
	// unadopted selector fails fast instead of leaving a dangling worktree.
	var linkTarget *wkitem.WorkItem
	if createWorkitem != "" {
		linkTarget, err = selector.Resolve(ctx, campRoot, createWorkitem, selector.ResolveOptions{})
		if err != nil {
			return camperrors.Wrap(err, "resolve workitem "+createWorkitem)
		}
		if wkitem.NeedsAdoption(linkTarget) {
			return wkitem.NotAdoptedError(linkTarget.RelativePath)
		}
	}

	// Build options based on new semantics:
	// - Default: create new branch with worktree name, based on current branch
	// - --branch: checkout existing branch
	// - --start-point: specify base for new branch
	// - --track: track remote branch
	opts := &worktree.CreateOptions{
		Project:     projectName,
		ProjectPath: resolved.Path,
		Name:        worktreeName,
		TrackRemote: createTrack,
	}

	if createBranch != "" {
		// Explicit existing branch requested
		opts.Branch = createBranch
		opts.NewBranch = false
	} else if createTrack != "" {
		// Track remote branch (handled by TrackRemote)
		opts.NewBranch = false
	} else {
		// Default: create new branch based on current branch
		opts.NewBranch = true
		opts.Branch = worktreeName

		// Determine start point
		if createStartPoint != "" {
			opts.StartPoint = createStartPoint
		} else {
			// Get current branch as default start point
			git := worktree.NewGitWorktree(resolved.Path)
			currentBranch, err := git.CurrentBranch(ctx)
			if err != nil {
				return camperrors.Wrap(err, "failed to detect current branch")
			}
			opts.StartPoint = currentBranch
		}
	}

	// Execute creation
	result, err := creator.Create(ctx, opts)
	if err != nil {
		if errors.Is(err, worktree.ErrBranchExists) {
			return camperrors.Wrap(err, fmt.Sprintf(
				"branch %q already exists (a previous worktree may have been removed "+
					"without deleting its branch); reuse it with --branch %s, choose a "+
					"different name, or delete it with 'git branch -D %s'",
				opts.Branch, opts.Branch, opts.Branch))
		}
		return err
	}

	// Success output
	fmt.Println(ui.Success(fmt.Sprintf("Created worktree: %s/%s", result.Project, result.Name)))
	fmt.Printf("  Path:   %s\n", ui.Value(result.Path))
	fmt.Printf("  Branch: %s\n", ui.Value(result.Branch))

	if linkTarget != nil {
		link, lerr := attachWorktreeLink(ctx, campRoot, linkTarget, filepath.ToSlash(result.RelativePath))
		if lerr != nil {
			return camperrors.Wrap(lerr, "worktree created but workitem link failed")
		}
		fmt.Printf("  Workitem: %s (%s)\n", ui.Value(link.WorkitemID), ui.Dim(link.WorkitemKey))
		fmt.Println(ui.Dim("  camp p commit in this worktree will include WI-* in the campaign tag"))
	}

	projected, err := intskills.ProjectIntoWorktreeBestEffort(ctx, campRoot, result.Path)
	if err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("  Skills: could not project into worktree: %v", err)))
		fmt.Println(ui.Dim("  Fix later with: camp skills link --worktrees-only"))
	} else if projected {
		fmt.Println(ui.Dim("  Skills: projected campaign skill bundles into worktree (.agents/.claude/.grok)"))
	}

	fmt.Println()
	fmt.Println(ui.Dim(fmt.Sprintf("To navigate: cd %s", result.RelativePath)))

	return nil
}

// attachWorktreeLink attaches a primary worktree link for an already-resolved
// workitem so the resolver (and therefore camp p commit) picks up the workitem
// ref inside that tree.
func attachWorktreeLink(ctx context.Context, campRoot string, wi *wkitem.WorkItem, relativeWorktreePath string) (links.Link, error) {
	return links.AttachPrimary(ctx, campRoot, links.AttachOptions{
		WorkitemID:  wkitem.LinkWorkitemID(wi),
		WorkitemKey: wi.Key,
		Scope: links.LinkScope{
			Kind: links.ScopeWorktree,
			Path: relativeWorktreePath,
		},
		CreatedBy: "camp_worktrees_create",
		Replace:   true,
	})
}
