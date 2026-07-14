package project

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/paths"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
	"github.com/spf13/cobra"
)

var projectCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit changes in a project submodule",
	Long: `Commit changes within a project submodule.

Auto-detects the current project from your working directory,
or use --project to specify a project by name.

Examples:
  # From within a project directory
  cd projects/my-api
  camp project commit -m "Fix bug"

  # Specify project by name
  camp project commit --project my-api -m "Update deps"`,
	RunE: runProjectCommit,
}

var (
	projectCommitProject   string
	projectCommitMessages  []string
	projectCommitAll       bool
	projectCommitAmend     bool
	projectCommitSync      bool
	projectCommitNoSync    bool
	projectCommitAutoWrite bool
	projectCommitWorkitem  string
)

func init() {
	projectCommitCmd.Flags().StringVarP(&projectCommitProject, "project", "p", "", "Project name (auto-detected from cwd if not specified)")
	projectCommitCmd.Flags().StringArrayVarP(&projectCommitMessages, "message", "m", nil, "Commit message (repeatable; multiple -m are joined git-style into subject + body; required unless --auto-write)")
	projectCommitCmd.Flags().BoolVarP(&projectCommitAll, "all", "a", true, "Stage all changes")
	projectCommitCmd.Flags().BoolVar(&projectCommitAmend, "amend", false, "Amend the previous commit")
	projectCommitCmd.Flags().BoolVar(&projectCommitSync, "sync", false, "Sync submodule ref at campaign root after commit (also enabled by commit.sync_project_refs setting)")
	projectCommitCmd.Flags().BoolVar(&projectCommitNoSync, "no-sync", false, "Do not sync submodule ref even if settings enable it")
	projectCommitCmd.Flags().BoolVar(&projectCommitAutoWrite, "auto-write", false, "Run configured commit message writer")
	projectCommitCmd.Flags().StringVar(&projectCommitWorkitem, "workitem", "", "explicit workitem selector for the commit tag (overrides cwd-based resolution)")

	if err := projectCommitCmd.RegisterFlagCompletionFunc("project", cmdutil.CompleteProjectName); err != nil {
		panic(err)
	}

	Cmd.AddCommand(projectCommitCmd)
}

func runProjectCommit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Join repeated -m values git-style before any tag prepending so the tag
	// lands on the subject line.
	projectCommitMessage := commitkit.JoinMessages(projectCommitMessages)

	// Find campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Load campaign config once (used for worktree detection, the commit tag,
	// and the optional parent-ref sync).
	cfg, _ := config.LoadCampaignConfig(ctx, campRoot)

	// Resolve the working directory to commit in. With no explicit --project and
	// a cwd inside a worktree, commit in the worktree itself: the generic project
	// resolver only understands projects/<name> and submodule roots, so a commit
	// from projects/worktrees/<project>/<name> would otherwise fail to resolve.
	var (
		resolvedPath string
		relPath      string
		inWorktree   bool
	)
	if projectCommitProject == "" && cfg != nil {
		if wtCtx, derr := worktree.NewDetector(paths.NewResolver(campRoot, cfg.Paths())).DetectFromCwd(); derr == nil {
			resolvedPath = wtCtx.WorktreePath
			relPath, _ = filepath.Rel(campRoot, resolvedPath)
			inWorktree = true
			fmt.Printf("Worktree: %s\n", ui.Value(wtCtx.Project+"/"+wtCtx.WorktreeName))
		}
	}
	if !inWorktree {
		result, rerr := projectsvc.Resolve(ctx, campRoot, projectCommitProject)
		if rerr != nil {
			var notFound *projectsvc.ProjectNotFoundError
			if errors.As(rerr, &notFound) {
				fmt.Println(ui.Dim("\n" + projectsvc.FormatProjectList(notFound.AvailableProjects())))
			}
			return rerr
		}
		if rgErr := result.RequireGit("git commits"); rgErr != nil {
			return rgErr
		}
		resolvedPath = result.Path
		relPath = result.LogicalPath
		if relPath == "" {
			relPath, _ = filepath.Rel(campRoot, resolvedPath)
		}
		fmt.Printf("Project: %s\n", ui.Value(relPath))
	}

	// Create executor for the submodule
	executor, err := git.NewExecutor(resolvedPath)
	if err != nil {
		return camperrors.Wrap(err, "failed to initialize git")
	}

	if projectCommitAutoWrite && projectCommitMessage != "" {
		return camperrors.Newf("--auto-write cannot be used with --message")
	}

	// Get commit message - prompt if not provided
	message := projectCommitMessage
	if !projectCommitAutoWrite && message == "" && !projectCommitAmend {
		var promptErr error
		message, promptErr = ui.PromptCommitMessageSimple(ctx, executor, false)
		if promptErr != nil {
			return camperrors.Wrap(promptErr, "prompt failed")
		}
		if message == "" {
			return git.ErrCommitCancelled
		}
	}

	// Stage if requested
	if projectCommitAll {
		fmt.Println(ui.Info("Staging changes..."))
		if err := executor.StageAll(ctx); err != nil {
			return camperrors.Wrap(err, "failed to stage")
		}
	}

	// Check for changes
	hasChanges, err := executor.HasChanges(ctx)
	if err != nil {
		return err
	}
	if !hasChanges && !projectCommitAmend {
		fmt.Println(ui.Success("Nothing to commit in project"))
		return nil
	}

	// Show what's staged
	cmdutil.ShowStagedSummary(ctx, resolvedPath)

	if projectCommitAutoWrite {
		fmt.Println(ui.Info("Writing commit message..."))
		var hookErr error
		extraEnv := workitemEnvForProjectCommit(ctx, campRoot, resolvedPath, projectCommitWorkitem)
		message, hookErr = commitkit.AutoWriteCommitMessageWithEnv(ctx, campRoot, resolvedPath, extraEnv)
		if hookErr != nil {
			return hookErr
		}
	}

	// Resolve commit prefs before committing so a malformed commit config fails
	// the commit instead of silently switching policy — in particular so a
	// corrupt local.json cannot inherit a global sync_project_refs and turn this
	// project commit into an unexpected campaign-root pointer commit below. A
	// missing config is not an error (loaders return defaults).
	commitPrefs, err := config.EffectiveCommitPrefs(ctx, campRoot)
	if err != nil {
		return err
	}
	// Prepend campaign tag unless tracing is disabled. Resolves the active
	// workitem so the tag includes WI-<ref> when the project is linked.
	if cfg != nil && commitPrefs.TagCommits() {
		questID, workitemRef := resolveProjectCommitContext(ctx, campRoot, resolvedPath, projectCommitWorkitem)
		message = commitkit.PrependContextTagsFullNamed(cfg.Name, cfg.ID, questID, "", workitemRef, message)
	}

	// Commit
	fmt.Println(ui.Info("Committing changes..."))
	opts := &git.CommitOptions{
		Message: message,
		Amend:   projectCommitAmend,
	}
	if err := executor.Commit(ctx, opts); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			fmt.Println(ui.Success("Nothing to commit"))
			return nil
		}
		return camperrors.Wrap(err, "commit failed")
	}

	fmt.Println(ui.Success("✓ Project changes committed"))

	// One emitter for the whole invocation so the project commit and any root
	// pointer-sync commit share a single action id (D002).
	emitter := ledger.NewFromRoot(ctx, campRoot, ledger.WarnTo(cmd.ErrOrStderr()))
	if sha, shaErr := commitkit.ShortHash(ctx, resolvedPath); shaErr == nil {
		emitter.CommitEvidence(ctx, ledgerkit.Scope{}, campRoot, resolvedPath, sha, message)
	}

	// Sync submodule ref in campaign root when enabled by --sync or by the
	// commit.sync_project_refs setting (and not disabled by --no-sync). A
	// worktree commit lands on its own branch under the gitignored worktrees
	// dir, so there is no submodule ref to sync at the campaign root.
	doSync := (commitPrefs.SyncProjectRefs || projectCommitSync) && !projectCommitNoSync
	if doSync && !inWorktree && git.HasPathDiff(ctx, campRoot, resolvedPath) {
		if err := syncParentRef(ctx, campRoot, relPath, cfg, emitter, commitPrefs); err != nil {
			fmt.Println()
			fmt.Println(ui.Warning("Could not auto-sync campaign root: " + err.Error()))
			fmt.Println(ui.Dim("Run 'camp commit' to update manually."))
		}
	}

	return nil
}

// syncParentRef stages and commits the submodule ref update in the campaign root.
func syncParentRef(ctx context.Context, campRoot, relPath string, cfg *config.CampaignConfig, emitter *ledger.Emitter, prefs config.CommitPrefs) error {
	if err := git.StageFiles(ctx, campRoot, relPath); err != nil {
		return camperrors.Wrap(err, "staging submodule ref")
	}
	hasRefChange, err := git.HasStagedPathChange(ctx, campRoot, relPath)
	if err != nil {
		return camperrors.Wrap(err, "check staged submodule ref")
	}
	if !hasRefChange {
		return nil
	}

	projName := filepath.Base(relPath)
	msg := fmt.Sprintf("update %s submodule ref", projName)
	if cfg != nil && prefs.TagCommits() {
		msg = git.PrependContextTagsFull(cfg.Name, cfg.ID, "", "", "", msg)
	}

	opts := &git.CommitOptions{Message: msg}
	if err := git.CommitScoped(ctx, campRoot, []string{relPath}, opts); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			return nil
		}
		return camperrors.Wrap(err, "commit")
	}

	if emitter != nil {
		if sha, shaErr := commitkit.ShortHash(ctx, campRoot); shaErr == nil {
			emitter.CommitEvidence(ctx, ledgerkit.Scope{}, campRoot, campRoot, sha, msg)
		}
	}

	fmt.Println(ui.Success("✓ Campaign root synced (" + relPath + ")"))
	return nil
}
