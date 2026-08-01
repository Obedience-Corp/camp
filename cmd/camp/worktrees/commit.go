package worktrees

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Obedience-Corp/camp/internal/defercommit"
	"github.com/Obedience-Corp/camp/internal/drain"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
	"github.com/spf13/cobra"
)

var (
	wtCommitMessages  []string
	wtCommitAll       bool
	wtCommitAmend     bool
	wtCommitAutoWrite bool
	wtCommitNoDrain   bool
	wtCommitWorkitem  string
	wtCommitLarge     bool
)

var worktreesCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit changes in a worktree",
	Long: `Commit changes within the current worktree.

Auto-detects the worktree from your current directory.

Commit tags use explicit --workitem or context from the current path. They do
not inherit the per-machine current workitem selection, which can be stale;
use 'camp workitem commit' when you want current.yaml scoping.

Examples:
  # From within a worktree
  cd projects/worktrees/my-api/feature-auth
  camp worktrees commit -m "Add login feature"

  # With all changes staged
  camp worktrees commit -m "Update deps" --all

  # Amend previous commit
  camp worktrees commit --amend -m "Fix typo"`,
	RunE: runWorktreesCommit,
}

func init() {
	Cmd.AddCommand(worktreesCommitCmd)

	worktreesCommitCmd.Flags().StringArrayVarP(&wtCommitMessages, "message", "m", nil,
		"Commit message (repeatable; multiple -m are joined git-style into subject + body; required unless --auto-write)")
	worktreesCommitCmd.Flags().BoolVarP(&wtCommitAll, "all", "a", true,
		"Stage all changes before committing")
	worktreesCommitCmd.Flags().BoolVar(&wtCommitAmend, "amend", false,
		"Amend the previous commit")
	worktreesCommitCmd.Flags().BoolVar(&wtCommitNoDrain, "no-drain", false,
		"Do not wait for camp's queued commits first")
	worktreesCommitCmd.Flags().BoolVar(&wtCommitAutoWrite, "auto-write", false,
		"Run configured commit message writer")
	worktreesCommitCmd.Flags().BoolVar(&wtCommitLarge, "commit-large", false,
		"Commit over-threshold files instead of keeping them out of git")
	worktreesCommitCmd.Flags().StringVar(&wtCommitWorkitem, "workitem", "",
		"explicit workitem selector for the commit tag (overrides cwd-based resolution)")
}

func runWorktreesCommit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Join repeated -m values git-style before any tag prepending so the tag
	// lands on the subject line.
	wtCommitMessage := commitkit.JoinMessages(wtCommitMessages)

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "failed to load campaign config")
	}

	resolver := paths.NewResolver(campRoot, cfg.Paths())
	detector := worktree.NewDetector(resolver)

	// Detect worktree from cwd
	wtCtx, err := detector.DetectFromCwd()
	if err != nil {
		return camperrors.Wrap(err, "not inside a worktree")
	}

	// Display which worktree
	fmt.Printf("Worktree: %s/%s\n", ui.Value(wtCtx.Project), ui.Value(wtCtx.WorktreeName))
	fmt.Printf("Branch:   %s\n", ui.Value(wtCtx.Branch))
	fmt.Println()

	// A worktree is its own lane, and this command writes history in it. It
	// was the one commit surface with no drain, so a queued commit there could
	// land in the middle of the user's.
	if !wtCommitNoDrain {
		if _, err := drain.Repo(ctx, wtCtx.WorktreePath, drain.Commit); err != nil {
			return err
		}
	}

	// Create executor for the worktree
	executor, err := git.NewExecutor(wtCtx.WorktreePath)
	if err != nil {
		return camperrors.Wrap(err, "failed to initialize git")
	}

	if wtCommitAutoWrite && wtCommitMessage != "" {
		return camperrors.Newf("--auto-write cannot be used with --message")
	}

	// Get commit message - prompt if not provided
	message := wtCommitMessage
	if !wtCommitAutoWrite && message == "" && !wtCommitAmend {
		message, err = ui.PromptCommitMessageSimple(ctx, executor, false)
		if err != nil {
			return camperrors.Wrap(err, "prompt failed")
		}
		if message == "" {
			return camperrors.Newf("commit cancelled")
		}
	}

	// Stage if requested
	if wtCommitAll {
		fmt.Println(ui.Info("Staging changes..."))
		empty, stageErr := stageWorktreeWithGuard(ctx, cmd, campRoot, wtCtx.WorktreePath)
		if stageErr != nil {
			return camperrors.Wrap(stageErr, "failed to stage")
		}
		if empty && !wtCommitAmend {
			cmdutil.ReportNothingLeftToCommit(cmd.OutOrStdout())
			return nil
		}
	} else {
		// No staging means the guard never ran. Report what the index already
		// holds; nothing here changes the commit.
		cmdutil.ReportStagedGrowth(ctx, cmd.OutOrStdout(), wtCtx.WorktreePath, wtCommitLarge)
	}

	// Check for changes
	hasChanges, err := executor.HasChanges(ctx)
	if err != nil {
		return err
	}
	if !hasChanges && !wtCommitAmend {
		fmt.Println(ui.Success("Nothing to commit in worktree"))
		return nil
	}

	// Show what's staged
	cmdutil.ShowStagedSummary(ctx, wtCtx.WorktreePath)

	// Deferral point. A worktree is its own lane, so a queued commit here
	// cannot serialize against the project it belongs to.
	if wtCommitAutoWrite {
		deferred, deferErr := cmdutil.TryDeferAutoWrite(
			ctx, os.Stdout, campRoot, wtCtx.WorktreePath, false, wtCommitAmend,
			defercommit.EnqueueOptions{
				WriterEnv: workitemEnvForWorktreeCommit(
					ctx, campRoot, wtCtx.WorktreePath, wtCommitWorkitem),
				MessagePrefix: worktreeCommitTagPrefix(ctx, campRoot, wtCtx.WorktreePath),
			})
		if deferErr != nil {
			return deferErr
		}
		if deferred {
			return nil
		}
	}

	if wtCommitAutoWrite {
		fmt.Println(ui.Info("Writing commit message..."))
		extraEnv := workitemEnvForWorktreeCommit(ctx, campRoot, wtCtx.WorktreePath, wtCommitWorkitem)
		extraEnv = commitkit.WithCommitAmendEnv(extraEnv, wtCommitAmend)
		message, err = commitkit.AutoWriteCommitMessageWithEnv(ctx, campRoot, wtCtx.WorktreePath, extraEnv)
		if err != nil {
			return err
		}
	}

	// Prepend campaign/workitem context tag unless tracing is disabled. Resolve
	// before committing so a malformed commit config fails loudly here rather
	// than silently applying a default policy.
	prefs, err := config.EffectiveCommitPrefs(ctx, campRoot)
	if err != nil {
		return err
	}
	if prefs.TagCommits() {
		questID, festivalRef, workitemRef := resolveWorktreeCommitContext(ctx, campRoot, wtCtx.WorktreePath, wtCommitWorkitem)
		message = commitkit.PrependContextTagsFullNamed(cfg.Name, cfg.ID, questID, festivalRef, workitemRef, message)
	}

	// Commit
	fmt.Println(ui.Info("Committing changes..."))
	opts := &git.CommitOptions{
		Message: message,
		Amend:   wtCommitAmend,
	}
	if err := executor.Commit(ctx, opts); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			fmt.Println(ui.Success("Nothing to commit"))
			return nil
		}
		return camperrors.Wrap(err, "commit failed")
	}

	fmt.Println(ui.Success("Changes committed in worktree"))

	return nil
}

// stageWorktreeWithGuard stages a worktree through the guarded chokepoint and
// reports what the guard decided.
//
// A worktree is project scope, so camp never declares an artifact root here:
// it excludes, says what it left out, and offers the three ways forward. The
// reporting is shared with `camp commit` and `camp p commit` so the same
// situation never reads differently depending on which command found it.
// It reports whether the guard excluded everything there was to commit, which
// the caller needs to say "nothing left to commit" rather than falling through
// to git's generic "nothing to commit". A worktree holding only over-threshold
// files hits exactly that: the guard excludes them all, the index is empty, and
// without this the user is told their working tree is clean while looking at
// files they just created.
func stageWorktreeWithGuard(
	ctx context.Context, cmd *cobra.Command, campRoot, worktreePath string,
) (excludedEverything bool, err error) {
	outcome, err := git.StageWithGuardOptions(ctx, worktreePath, nil,
		git.StageOptions{CommitLarge: wtCommitLarge})
	if err != nil {
		return false, err
	}
	handling, err := cmdutil.HandleStageOutcome(ctx, cmd.OutOrStdout(), campRoot, worktreePath, outcome)
	if err != nil {
		return false, err
	}
	return cmdutil.GuardExcludedEverything(ctx, worktreePath, handling)
}

// worktreeCommitTagPrefix resolves the campaign tag a worktree commit carries,
// so a deferred one ends up with the same subject as its synchronous twin.
func worktreeCommitTagPrefix(ctx context.Context, campRoot, worktreePath string) string {
	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil || cfg == nil {
		return ""
	}
	prefs, err := config.EffectiveCommitPrefs(ctx, campRoot)
	if err != nil || !prefs.TagCommits() {
		return ""
	}
	questID, festivalRef, workitemRef := resolveWorktreeCommitContext(
		ctx, campRoot, worktreePath, wtCommitWorkitem)
	return commitkit.FormatContextTagsFullNamed(cfg.Name, cfg.ID, questID, festivalRef, workitemRef)
}
