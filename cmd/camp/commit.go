package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/defercommit"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jobs"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/drain"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
	"github.com/spf13/cobra"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit changes in the campaign root",
	Long: `Commit changes in the campaign root directory.

Automatically stages all changes and creates a commit. Handles
stale lock files from crashed processes.

At the campaign root, submodule ref changes (projects/*) are excluded
from staging by default to prevent accidental ref conflicts across
machines. Use --include-refs to stage them explicitly.

Use --sub to commit in the submodule detected from your current directory.
Use -p/--project to commit in a specific project (e.g., -p projects/camp).

Commit tags use explicit --workitem or context from the current path. They do
not inherit the per-machine current workitem selection, which can be stale;
use 'camp workitem commit' when you want current.yaml scoping.

Examples:
  camp commit -m "Add new feature"
  camp commit --amend -m "Fix typo"
  camp commit -a -m "Stage and commit all"
  camp commit --include-refs -m "Sync all submodule refs"
  camp commit --sub -m "Commit in current submodule"
  camp commit -p projects/camp -m "Commit in camp project"`,
	RunE: runCommit,
}

var (
	commitMessages    []string
	commitAll         bool
	commitAmend       bool
	commitSub         bool
	commitProject     string
	commitIncludeRefs bool
	commitAutoWrite   bool
	commitWorkitem    string
	commitNoEdit      bool
	commitLarge       bool
	commitJSONOut     bool
	commitNoDrain     bool
)

func init() {
	commitCmd.Flags().StringArrayVarP(&commitMessages, "message", "m", nil, "Commit message (repeatable; multiple -m are joined git-style into subject + body; required unless --auto-write)")
	commitCmd.Flags().BoolVarP(&commitAll, "all", "a", true, "Stage all changes before committing")
	commitCmd.Flags().BoolVar(&commitAmend, "amend", false, "Amend the previous commit")
	commitCmd.Flags().BoolVar(&commitNoEdit, "no-edit", false, "Amend without editing the commit message (requires --amend)")
	commitCmd.Flags().BoolVar(&commitSub, "sub", false, "Operate on the submodule detected from current directory")
	commitCmd.Flags().StringVarP(&commitProject, "project", "p", "", "Operate on a specific project/submodule path")
	commitCmd.Flags().BoolVar(&commitIncludeRefs, "include-refs", false, "Include submodule ref changes when staging at campaign root")
	commitCmd.Flags().BoolVar(&commitAutoWrite, "auto-write", false, "Run configured commit message writer")
	commitCmd.Flags().StringVar(&commitWorkitem, "workitem", "", "explicit workitem selector for the commit tag (overrides cwd-based resolution)")
	commitCmd.Flags().BoolVar(&commitLarge, "commit-large", false, "Commit over-threshold files instead of keeping them out of git")
	commitCmd.Flags().BoolVar(&commitJSONOut, "json", false, "Emit a JSON result on stdout; human output goes to stderr")
	commitCmd.Flags().BoolVar(&commitNoDrain, "no-drain", false, "Do not wait for camp's queued commits first")

	rootCmd.AddCommand(commitCmd)
	commitCmd.GroupID = "git"

	// Register completion for --project flag
	commitCmd.RegisterFlagCompletionFunc("project", completeProjectFlag)
}

// commitTagPrefix resolves the campaign tag this commit would carry.
//
// A deferred commit must end up with the same subject line its synchronous
// twin would have, and the tag depends on the campaign, quest, festival, and
// workitem resolved from the user's working directory. The worker has none of
// that, so the prefix is computed here and recorded on the job.
func commitTagPrefix(ctx context.Context, campRoot string) string {
	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil || cfg == nil {
		return ""
	}
	prefs, err := config.EffectiveCommitPrefs(ctx, campRoot)
	if err != nil || !prefs.TagCommits() {
		return ""
	}
	questID, festivalRef, workitemRef := resolveCommitContext(ctx, campRoot, commitWorkitem)
	return commitkit.FormatContextTagsFullNamed(cfg.Name, cfg.ID, questID, festivalRef, workitemRef)
}

// completeProjectFlag provides tab completion for the --project flag.
func completeProjectFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	paths, err := git.ListSubmodulePathsFiltered(ctx, campRoot, toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return paths, cobra.ShellCompDirectiveNoFileComp
}

func runCommit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Under --json, stdout carries exactly one document and nothing else.
	// Human lines go to stderr rather than being dropped, so a person watching
	// a scripted run still sees what happened. fest has the interleave bug
	// this avoids; the point of doing it here is not to recreate it.
	humanOut := cmd.OutOrStdout()
	if commitJSONOut {
		humanOut = cmd.ErrOrStderr()
	}
	jsonResult := newCommitJSONResult("")

	// Join repeated -m values git-style before any tag prepending so the tag
	// lands on the subject line.
	commitMessage := commitkit.JoinMessages(commitMessages)

	// Find campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	// Resolve target repository
	target, err := git.ResolveTarget(ctx, campRoot, commitSub, commitProject)
	if err != nil {
		return camperrors.Wrap(err, "failed to resolve target")
	}

	if target.IsSubmodule {
		_, _ = fmt.Fprintln(humanOut, ui.Info(fmt.Sprintf("Operating on submodule: %s", target.Name)))
	}

	if commitAutoWrite && commitMessage != "" {
		return camperrors.Newf("--auto-write cannot be used with --message")
	}
	if commitNoEdit && !commitAmend {
		return camperrors.New("--no-edit requires --amend")
	}
	if commitAmend && commitMessage == "" && !commitAutoWrite && !commitNoEdit {
		return camperrors.New("amend without a message requires --no-edit or --message")
	}

	// Drain before anything is staged, so the barrier holds: everything
	// enqueued before this command is in history before the user's commit
	// rather than interleaved with it. Job-aware, because a deferred
	// --auto-write message writer is unbounded and refusing the user's next
	// commit for it would be camp's fault, not theirs.
	if !commitNoDrain {
		waited, err := drain.Repo(ctx, target.Path, drain.Commit)
		if err != nil {
			return err
		}
		jsonResult.DrainWaitedMs = waited.Milliseconds()
	}

	stageAll := effectiveCommitAll(cmd, commitAmend, commitAll)

	// Create executor
	executor, err := git.NewExecutor(target.Path)
	if err != nil {
		return camperrors.Wrap(err, "failed to initialize git")
	}

	// Get commit message - prompt if not provided
	message := commitMessage
	if !commitAutoWrite && message == "" && !commitAmend {
		var promptErr error
		message, promptErr = ui.PromptCommitMessageSimple(ctx, executor, !target.IsSubmodule && !commitIncludeRefs)
		if promptErr != nil {
			return camperrors.Wrap(promptErr, "prompt failed")
		}
		if message == "" {
			return git.ErrCommitCancelled
		}
	}

	// Stage if requested
	if stageAll {
		_, _ = fmt.Fprintln(humanOut, ui.Info("Staging changes..."))
		// The guard error is routed through the JSON reporter for both
		// branches, hoisted below. A submodule commit that returned it raw
		// emitted no document under --json, so a machine caller saw a bare
		// non-zero exit where the campaign-root branch gave it a refusal it
		// could act on.
		var (
			guardOutcome *git.StageOutcome
			stageErr     error
		)
		if target.IsSubmodule || commitIncludeRefs {
			guardOutcome, stageErr = git.StageWithGuardOptions(
				ctx, target.Path, nil, git.StageOptions{CommitLarge: commitLarge})
		} else {
			// Campaign root: exclude submodule refs to prevent accidental
			// ref changes from polluting content commits.
			paths, pathErr := git.ListSubmodulePaths(ctx, target.Path)
			if pathErr != nil {
				return pathErr
			}
			guardOutcome, stageErr = git.StageAllExcludingWithGuardOptions(
				ctx, target.Path, paths, git.StageOptions{CommitLarge: commitLarge})
			if stageErr == nil {
				// git add rejects exclude pathspecs whose target contains
				// gitignored entries, so worktrees are unstaged after staging
				// instead of excluded up front.
				if wt := worktreesRelPath(ctx, campRoot); wt != "" {
					if err := git.UnstagePath(ctx, target.Path, wt); err != nil {
						return err
					}
				}
			}
		}
		if stageErr != nil {
			return reportGuardRefusalJSON(cmd, jsonResult, stageErr)
		}

		// Act on what the guard decided and tell the user. Runs after staging
		// so the declaration it may write lands in this same commit.
		handling, err := cmdutil.HandleStageOutcome(ctx, humanOut, campRoot, target.Path, guardOutcome)
		if err != nil {
			return err
		}
		jsonResult.applyGuardHandling(handling)
		// The excluded file is still an unstaged worktree change, so the
		// ordinary has-changes check would say yes and git would then reject
		// the empty commit. Report the no-op instead of failing.
		empty, err := cmdutil.GuardExcludedEverything(ctx, target.Path, handling)
		if err != nil {
			return err
		}
		if empty && !commitAmend {
			cmdutil.ReportNothingLeftToCommit(humanOut)
			return commitJSONNoop(cmd, jsonResult)
		}
	} else {
		// Nothing was staged here, so the guard at the staging chokepoint never
		// ran: the index is whatever the user built with git add. Report what it
		// holds, and only report -- the commit below is unchanged.
		cmdutil.ReportStagedGrowth(ctx, humanOut, target.Path, commitLarge)
	}

	// Show what will be committed
	cmdutil.ShowStagedSummaryTo(humanOut, ctx, target.Path)

	// Refuse root content commits that would accidentally sweep pre-staged
	// submodule gitlinks into this commit's message/tag context. The user can
	// make that coupling explicit with --include-refs or use refs-sync instead.
	if !target.IsSubmodule && !commitIncludeRefs {
		stagedRefs, refErr := listStagedProjectRefs(ctx, target.Path)
		if refErr != nil {
			return camperrors.Wrap(refErr, "check staged submodule refs")
		}
		if len(stagedRefs) > 0 {
			return camperrors.NewValidation("pre_staged_refs", preStagedRefsMessage(stagedRefs), nil)
		}
	}

	// Commit only what is staged. HasChanges also counts unstaged and
	// untracked dirt (e.g. excluded submodule pointers at campaign root), which
	// would let a deferred --auto-write enqueue an empty tree and land a no-op
	// commit. Staged is the only signal that matches what write-tree / commit
	// will actually record.
	hasStaged, err := git.HasStagedChanges(ctx, target.Path)
	if err != nil {
		return err
	}
	if !hasStaged && !commitAmend {
		if !target.IsSubmodule && !commitIncludeRefs {
			driftRefs, driftErr := listUnstagedProjectRefs(ctx, target.Path)
			if driftErr != nil {
				return camperrors.Wrap(driftErr, "check unstaged submodule refs")
			}
			if len(driftRefs) > 0 {
				_, _ = fmt.Fprintln(humanOut, ui.Warning("Nothing to commit (submodule ref changes are excluded by default)"))
				_, _ = fmt.Fprintln(humanOut, ui.Dim("  Use 'camp refs-sync' to commit only the submodule pointers."))
				_, _ = fmt.Fprintln(humanOut, ui.Dim("  Use 'camp commit --include-refs -m \"...\"' to include them in this commit."))
				return commitJSONNoop(cmd, jsonResult)
			}
		}
		// Staged-only gate: do not claim the worktree is clean when unstaged
		// or untracked dirt remains (e.g. excluded submodule pointers).
		_, _ = fmt.Fprintln(humanOut, ui.Success(cmdutil.NothingStagedLine(ctx, target.Path, "")))
		return commitJSONNoop(cmd, jsonResult)
	}

	// Deferral point. Everything the user watches has already happened:
	// staging, the guard, any artifact-root declaration. Only the message
	// writer and the commit object move to the background, which is the part
	// that can take an LLM call.
	if commitAutoWrite {
		deferred, deferErr := cmdutil.TryDeferAutoWrite(
			ctx, humanOut, campRoot, target.Path, commitJSONOut, commitAmend,
			defercommit.EnqueueOptions{
				WriterEnv:     workitemEnvForCommit(ctx, campRoot, commitWorkitem),
				MessagePrefix: commitTagPrefix(ctx, campRoot),
			})
		if errors.Is(deferErr, git.ErrNoChanges) {
			_, _ = fmt.Fprintln(humanOut, ui.Success("Nothing to commit"))
			return commitJSONNoop(cmd, jsonResult)
		}
		if deferErr != nil {
			return deferErr
		}
		if deferred {
			return nil
		}
	}

	if commitAutoWrite {
		_, _ = fmt.Fprintln(humanOut, ui.Info("Writing commit message..."))
		var hookErr error
		extraEnv := workitemEnvForCommit(ctx, campRoot, commitWorkitem)
		extraEnv = commitkit.WithCommitAmendEnv(extraEnv, commitAmend)
		message, hookErr = commitkit.AutoWriteCommitMessageWithEnv(ctx, campRoot, target.Path, extraEnv)
		if hookErr != nil {
			return hookErr
		}
	}

	// Prepend campaign tag (graceful degradation if config unavailable).
	// Resolves the active workitem (and any captured quest) so the tag
	// includes WI-<ref> when one is in context. Skip when commit tracing is
	// disabled via settings.
	if cfg, cfgErr := config.LoadCampaignConfig(ctx, campRoot); cfgErr == nil && message != "" {
		prefs, err := config.EffectiveCommitPrefs(ctx, campRoot)
		if err != nil {
			return err
		}
		if prefs.TagCommits() {
			questID, festivalRef, workitemRef := resolveCommitContext(ctx, campRoot, commitWorkitem)
			message = commitkit.PrependContextTagsFullNamed(cfg.Name, cfg.ID, questID, festivalRef, workitemRef, message)
		}
	}

	// Perform commit
	_, _ = fmt.Fprintln(humanOut, ui.Info("Committing changes..."))
	opts := &git.CommitOptions{
		Message: message,
		Amend:   commitAmend,
		NoEdit:  commitNoEdit,
	}

	if err := executor.Commit(ctx, opts); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			if !target.IsSubmodule && !commitIncludeRefs {
				driftRefs, driftErr := listUnstagedProjectRefs(ctx, target.Path)
				if driftErr != nil {
					return camperrors.Wrap(driftErr, "check unstaged submodule refs")
				}
				if len(driftRefs) > 0 {
					_, _ = fmt.Fprintln(humanOut, ui.Warning("Nothing to commit (submodule ref changes are excluded by default)"))
					_, _ = fmt.Fprintln(humanOut, ui.Dim("  Use 'camp refs-sync' to commit only the submodule pointers."))
					_, _ = fmt.Fprintln(humanOut, ui.Dim("  Use 'camp commit --include-refs -m \"...\"' to include them in this commit."))
					return nil
				}
			}
			_, _ = fmt.Fprintln(humanOut, ui.Success("Nothing to commit"))
			return nil
		}
		return err
	}

	// Record the landed commit as ledger evidence (D003: after the commit
	// exists). Tagging and the message above are untouched.
	if sha, shaErr := commitkit.ShortHash(ctx, target.Path); shaErr == nil {
		_, festivalRef, workitemRef := resolveCommitContext(ctx, campRoot, commitWorkitem)
		ledger.NewFromRoot(ctx, campRoot, ledger.WarnTo(cmd.ErrOrStderr())).
			CommitEvidence(ctx, ledgerkit.Scope{Workitem: workitemRef, Festival: festivalRef}, campRoot, target.Path, sha, message)
	}

	// Committed artifact manifests: re-queue each declared root's record so
	// it describes the commit that just landed. Enqueue writes queue files
	// only; all hashing happens in the worker, so the command returns before
	// any of it begins. A failure to queue warns rather than failing a commit
	// that already succeeded: the record is eventually consistent, and the
	// next commit re-queues it.
	if !target.IsSubmodule {
		if n, mErr := jobs.EnqueueManifestsForRoots(ctx, campRoot); mErr != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), ui.Warning("artifact manifest not queued: "+mErr.Error()))
		} else if n > 0 {
			defercommit.SpawnWorker(ctx, campRoot, campRoot)
		}
	}

	_, _ = fmt.Fprintln(humanOut, ui.Success("Changes committed successfully"))

	if commitJSONOut {
		jsonResult.Repo = target.Path
		if err := jsonResult.finalize(ctx, target.Path); err != nil {
			return err
		}
		return jsonResult.emit(cmd.OutOrStdout())
	}
	return nil
}

// commitJSONNoop emits the document for a run that legitimately produced no
// commit, so a machine caller always receives one document per invocation
// rather than having to treat "no output" as a distinct outcome.
func commitJSONNoop(cmd *cobra.Command, result *commitJSONResult) error {
	if !commitJSONOut {
		return nil
	}
	return result.emit(cmd.OutOrStdout())
}

func worktreesRelPath(ctx context.Context, campRoot string) string {
	worktreesPath := config.DefaultCampaignPaths().Worktrees
	if cfg, err := config.LoadCampaignConfig(ctx, campRoot); err == nil {
		if wt := cfg.Paths().Worktrees; wt != "" {
			worktreesPath = wt
		}
	}
	return strings.Trim(strings.TrimSpace(worktreesPath), "/")
}

func effectiveCommitAll(cmd *cobra.Command, amend, all bool) bool {
	if amend && !cmd.Flags().Changed("all") {
		return false
	}
	return all
}

func listStagedProjectRefs(ctx context.Context, repoPath string) ([]string, error) {
	paths, err := git.ListSubmodulePaths(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	var staged []string
	for _, path := range paths {
		if !strings.HasPrefix(path, "projects/") {
			continue
		}
		hasChange, err := git.HasStagedPathChange(ctx, repoPath, path)
		if err != nil {
			return nil, err
		}
		if hasChange {
			staged = append(staged, path)
		}
	}
	return staged, nil
}

func listUnstagedProjectRefs(ctx context.Context, repoPath string) ([]string, error) {
	paths, err := git.ListSubmodulePaths(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	var drift []string
	for _, path := range paths {
		if strings.HasPrefix(path, "projects/") && git.HasPathDiff(ctx, repoPath, path) {
			drift = append(drift, path)
		}
	}
	return drift, nil
}

func preStagedRefsMessage(paths []string) string {
	joined := strings.Join(paths, ", ")
	resetPaths := strings.Join(paths, " ")
	return fmt.Sprintf(
		"staged submodule ref(s) found: %s\n"+
			"These are not committed by 'camp commit' without --include-refs.\n"+
			"Options:\n"+
			"  camp refs-sync                         -- commit only the submodule pointers\n"+
			"  camp commit --include-refs -m \"...\"   -- include them in this commit\n"+
			"  git reset HEAD %s                      -- unstage them to continue",
		joined,
		resetPaths,
	)
}
