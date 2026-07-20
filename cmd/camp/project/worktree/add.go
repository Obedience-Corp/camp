package worktree

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/project"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
	intworktree "github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	wtAddProject    string
	wtAddBranch     string
	wtAddStartPoint string
	wtAddTrack      string
	wtAddWorkitem   string
)

var projectWorktreeAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new worktree for the project",
	Long: `Create a new git worktree for the current project.

Auto-detects the project from your current directory, or use --project
to specify explicitly.

The worktree will be created at: projects/worktrees/<project>/<name>/

By default, creates a new branch with the worktree name based on the current branch.
Use --branch to checkout an existing branch instead.

Examples:
  # Create worktree with new branch based on current branch (default)
  camp project worktree add feature-auth

  # Create worktree with new branch based on main
  camp project worktree add experiment --start-point main

  # Checkout existing branch (instead of creating new)
  camp project worktree add hotfix --branch hotfix-123

  # Track a remote branch
  camp project worktree add pr-review --track origin/feature-xyz

  # Explicit project
  camp project worktree add feature --project my-api

  # Link a design/explore workitem so camp p commit in the worktree tags WI-*
  camp project worktree add fest-list-watch --project fest --workitem WI-2a7950
  camp project worktree add settings-tui --project camp --workitem workflow/design/camp-settings-tui`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectWorktreeAdd,
}

func init() {
	Cmd.AddCommand(projectWorktreeAddCmd)

	projectWorktreeAddCmd.Flags().StringVarP(&wtAddProject, "project", "p", "", "Project name (auto-detected from cwd if not specified)")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddBranch, "branch", "b", "", "Checkout existing branch instead of creating new one")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddStartPoint, "start-point", "s", "", "Base branch/commit for new branch (default: current branch)")
	projectWorktreeAddCmd.Flags().StringVarP(&wtAddTrack, "track", "t", "", "Remote branch to track (creates new local tracking branch)")
	projectWorktreeAddCmd.Flags().StringVar(&wtAddWorkitem, "workitem", "", "workitem selector (ref, path, or id) to primary-link to this worktree for camp p commit tags")

	if err := projectWorktreeAddCmd.RegisterFlagCompletionFunc("project", cmdutil.CompleteProjectName); err != nil {
		panic(err)
	}
}

func runProjectWorktreeAdd(cmd *cobra.Command, args []string) error {
	worktreeName := args[0]
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "failed to load campaign config")
	}

	resolved, err := project.Resolve(ctx, campRoot, wtAddProject)
	if err != nil {
		var notFound *project.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}
	projectName := resolved.Name
	if err := resolved.RequireGit("git worktrees"); err != nil {
		return err
	}

	// Resolve and validate the workitem before creating anything, so a bad or
	// unadopted selector fails fast instead of leaving a dangling worktree.
	var linkTarget *wkitem.WorkItem
	if wtAddWorkitem != "" {
		linkTarget, err = selector.Resolve(ctx, campRoot, wtAddWorkitem, selector.ResolveOptions{})
		if err != nil {
			return camperrors.Wrap(err, "resolve workitem "+wtAddWorkitem)
		}
		if wkitem.NeedsAdoption(linkTarget) {
			return wkitem.NotAdoptedError(linkTarget.RelativePath)
		}
	}

	resolver := paths.NewResolver(campRoot, cfg.Paths())
	creator := intworktree.NewCreator(resolver, cfg)
	opts := &intworktree.CreateOptions{
		Project:     projectName,
		ProjectPath: resolved.Path,
		Name:        worktreeName,
		TrackRemote: wtAddTrack,
	}

	if wtAddBranch != "" {
		opts.Branch = wtAddBranch
		opts.NewBranch = false
	} else if wtAddTrack != "" {
		opts.NewBranch = false
	} else {
		opts.NewBranch = true
		opts.Branch = worktreeName

		if wtAddStartPoint != "" {
			opts.StartPoint = wtAddStartPoint
		} else {
			git := intworktree.NewGitWorktree(resolved.Path)
			currentBranch, err := git.CurrentBranch(ctx)
			if err != nil {
				return camperrors.Wrap(err, "failed to detect current branch")
			}
			opts.StartPoint = currentBranch
		}
	}

	result, err := creator.Create(ctx, opts)
	if err != nil {
		if errors.Is(err, intworktree.ErrBranchExists) {
			return camperrors.Wrap(err, fmt.Sprintf(
				"branch %q already exists (a previous worktree may have been removed "+
					"without deleting its branch); reuse it with --branch %s, choose a "+
					"different name, or delete it with 'git branch -D %s'",
				opts.Branch, opts.Branch, opts.Branch))
		}
		return err
	}

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

	// Project campaign skills into the worktree so Grok/Claude sessions whose
	// git root is the worktree still discover .campaign/skills. Failure here
	// must not undo a successful worktree create.
	projected, err := intskills.ProjectIntoWorktreeBestEffort(campRoot, result.Path)
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

// attachWorktreeLink attaches a primary worktree link so the resolver
// (and therefore camp p commit) picks up the workitem ref inside that tree.
// The workitem is resolved and validated by the caller before the worktree is
// created, so a bad selector never leaves a dangling worktree behind.
func attachWorktreeLink(ctx context.Context, campRoot string, wi *wkitem.WorkItem, relativeWorktreePath string) (links.Link, error) {
	scopePath := relativeWorktreePath
	if scopePath == "" {
		return links.Link{}, camperrors.NewValidation("worktree", "missing worktree relative path", nil)
	}
	return links.AttachPrimary(ctx, campRoot, links.AttachOptions{
		WorkitemID:  wkitem.LinkWorkitemID(wi),
		WorkitemKey: wi.Key,
		Scope: links.LinkScope{
			Kind: links.ScopeWorktree,
			Path: scopePath,
		},
		CreatedBy: "camp_project_worktree_add",
		Replace:   true,
	})
}
