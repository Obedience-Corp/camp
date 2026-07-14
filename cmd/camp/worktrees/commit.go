package worktrees

import (
	"errors"
	"fmt"

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
	wtCommitWorkitem  string
)

var worktreesCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit changes in a worktree",
	Long: `Commit changes within the current worktree.

Auto-detects the worktree from your current directory.

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
	worktreesCommitCmd.Flags().BoolVar(&wtCommitAutoWrite, "auto-write", false,
		"Run configured commit message writer")
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
		if err := executor.StageAll(ctx); err != nil {
			return camperrors.Wrap(err, "failed to stage")
		}
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

	if wtCommitAutoWrite {
		fmt.Println(ui.Info("Writing commit message..."))
		extraEnv := workitemEnvForWorktreeCommit(ctx, campRoot, wtCtx.WorktreePath, wtCommitWorkitem)
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
		questID, workitemRef := resolveWorktreeCommitContext(ctx, campRoot, wtCtx.WorktreePath, wtCommitWorkitem)
		message = commitkit.PrependContextTagsFullNamed(cfg.Name, cfg.ID, questID, "", workitemRef, message)
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
