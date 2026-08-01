package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/defercommit"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jobs"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/drain"
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

Commit tags use explicit --workitem or context from the current path. They do
not inherit the per-machine current workitem selection, which can be stale;
use 'camp workitem commit' when you want current.yaml scoping.

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
	projectCommitLarge     bool
	projectCommitNoDrain   bool
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
	projectCommitCmd.Flags().BoolVar(&projectCommitLarge, "commit-large", false, "Commit over-threshold files instead of keeping them out of git")
	projectCommitCmd.Flags().BoolVar(&projectCommitNoDrain, "no-drain", false, "Do not wait for camp's queued commits first")

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

	// Drain this repository's lane before staging, so anything camp already
	// promised to commit here is in history rather than racing the user's
	// commit. A worktree drains as its own lane, which is what its own path
	// resolves to.
	if !projectCommitNoDrain {
		if _, err := drain.Repo(ctx, resolvedPath, drain.Commit); err != nil {
			return err
		}
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
		guardOutcome, stageErr := git.StageWithGuardOptions(ctx, resolvedPath, nil, git.StageOptions{CommitLarge: projectCommitLarge})
		if stageErr != nil {
			return camperrors.Wrap(stageErr, "failed to stage")
		}
		// A project exclusion is never silent: camp cannot fix a large file
		// here (an artifact root would keep it off the project's remote), so
		// the least it owes the user is saying what it left out and why.
		handling, hErr := cmdutil.HandleStageOutcome(ctx, cmd.OutOrStdout(), campRoot, resolvedPath, guardOutcome)
		if hErr != nil {
			return hErr
		}
		empty, eErr := cmdutil.GuardExcludedEverything(ctx, resolvedPath, handling)
		if eErr != nil {
			return eErr
		}
		if empty && !projectCommitAmend {
			cmdutil.ReportNothingLeftToCommit(cmd.OutOrStdout())
			return nil
		}
	} else {
		// No staging means the guard never ran. Report what the index already
		// holds; nothing here changes the commit.
		cmdutil.ReportStagedGrowth(ctx, cmd.OutOrStdout(), resolvedPath, projectCommitLarge)
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

	// Deferral point, same as camp commit: staging and the guard already ran
	// in the foreground; only the writer and the commit object move.
	if projectCommitAutoWrite {
		deferred, deferErr := cmdutil.TryDeferAutoWrite(
			ctx, os.Stdout, campRoot, resolvedPath, false, projectCommitAmend,
			projectDeferOptions(ctx, campRoot, resolvedPath, relPath, cfg))
		if deferErr != nil {
			return deferErr
		}
		if deferred {
			return nil
		}
	}

	if projectCommitAutoWrite {
		fmt.Println(ui.Info("Writing commit message..."))
		var hookErr error
		extraEnv := workitemEnvForProjectCommit(ctx, campRoot, resolvedPath, projectCommitWorkitem)
		extraEnv = commitkit.WithCommitAmendEnv(extraEnv, projectCommitAmend)
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
	// workitem so the tag includes WI-<ref> when the project is linked, or
	// FE-<ref> when the worktree's primary link resolves to a festival.
	if cfg != nil && commitPrefs.TagCommits() {
		questID, festivalRef, workitemRef := resolveProjectCommitContext(ctx, campRoot, resolvedPath, projectCommitWorkitem)
		message = commitkit.PrependContextTagsFullNamed(cfg.Name, cfg.ID, questID, festivalRef, workitemRef, message)
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

	msg := submoduleRefMessage(relPath, cfg, prefs)

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

// projectDeferOptions builds what a deferred project commit must carry.
//
// The campaign tag and the writer's environment both come from the user's
// working directory, and the follow-up encodes the sync decision their flags
// and settings made. A detached worker can re-derive none of the three.
//
// The follow-up is how commit.sync_project_refs survives deferral: the worker
// enqueues it only after the project commit lands, so the gitlink it records is
// the commit that just happened. There is deliberately no synchronous fallback
// when sync is enabled; delegated work does not come back to the terminal.
func projectDeferOptions(
	ctx context.Context,
	campRoot, resolvedPath, relPath string,
	cfg *config.CampaignConfig,
) defercommit.EnqueueOptions {
	opts := defercommit.EnqueueOptions{
		WriterEnv: workitemEnvForProjectCommit(ctx, campRoot, resolvedPath, projectCommitWorkitem),
	}

	prefs, err := config.EffectiveCommitPrefs(ctx, campRoot)
	if err != nil {
		return opts
	}
	if cfg != nil && prefs.TagCommits() {
		questID, festivalRef, workitemRef := resolveProjectCommitContext(
			ctx, campRoot, resolvedPath, projectCommitWorkitem)
		opts.MessagePrefix = commitkit.FormatContextTagsFullNamed(
			cfg.Name, cfg.ID, questID, festivalRef, workitemRef)
	}

	if (prefs.SyncProjectRefs || projectCommitSync) && !projectCommitNoSync && relPath != "" {
		opts.Then = &jobs.Follow{
			Kind:  jobs.KindCommitPaths,
			Repo:  ".",
			Paths: []string{filepath.ToSlash(relPath)},
			// Composed here, by the same function the synchronous path uses,
			// so the two cannot drift. The worker records what it is given.
			Message: submoduleRefMessage(relPath, cfg, prefs),
		}
	}
	return opts
}

// submoduleRefMessage is the commit message a submodule pointer update carries.
//
// One function, because the synchronous sync and the deferred follow-up must
// produce the same commit. A deferred pointer commit worded differently would
// let anyone read which code path ran off git history, which is an
// implementation detail that has no business being permanent.
//
// The tag carries no quest, festival, or workitem on purpose: a pointer update
// is bookkeeping about a commit that already carries that context, not work in
// its own right.
func submoduleRefMessage(relPath string, cfg *config.CampaignConfig, prefs config.CommitPrefs) string {
	msg := fmt.Sprintf("update %s submodule ref", filepath.Base(relPath))
	if cfg != nil && prefs.TagCommits() {
		msg = git.PrependContextTagsFull(cfg.Name, cfg.ID, "", "", "", msg)
	}
	return msg
}
