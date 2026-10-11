package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/defercommit"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// CommitBackgroundJSONVersion is an opt-in receipt, separate from the
// synchronous commit/v1alpha1 contract. Queued does not mean committed.
const CommitBackgroundJSONVersion = "commit-background/v1alpha1"

type backgroundCommitOutcome string

const (
	backgroundQueued          backgroundCommitOutcome = "queued"
	backgroundNothingToCommit backgroundCommitOutcome = "nothing_to_commit"
	backgroundRefused         backgroundCommitOutcome = "refused"
)

type backgroundCommitResult struct {
	SchemaVersion         string                  `json:"schema_version"`
	Outcome               backgroundCommitOutcome `json:"outcome"`
	Repo                  string                  `json:"repo"`
	JobID                 string                  `json:"job_id"`
	Staged                int                     `json:"staged"`
	Excluded              []excludedFileJSON      `json:"excluded"`
	ArtifactRootsDeclared []string                `json:"artifact_roots_declared"`
	DrainWaitedMs         int64                   `json:"drain_waited_ms"`
}

// Only the opt-in mode adopts structured errors; legacy commit --json keeps
// its existing success and error contracts.
func runCommitCommand(cmd *cobra.Command, args []string) error {
	err := runCommit(cmd, args)
	if err != nil && commitBackground && commitJSONOut {
		return jsoncontract.RenderError(cmd, CommitBackgroundJSONVersion, err)
	}
	return err
}

func validateBackgroundCommit(background, autoWrite, amend bool, message string) error {
	if !background {
		return nil
	}
	if !autoWrite || message != "" || amend {
		return camperrors.NewValidation("background", "--background requires --auto-write and cannot be used with --message or --amend", nil)
	}
	return nil
}

func requireBackgroundCommit(ctx context.Context, campRoot, repo string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// This explicit mode emits a job receipt, not the synchronous --json
	// document. All other deferral safeguards still apply.
	allowed, reason := defercommit.Allowed(ctx, defercommit.Request{
		CampaignRoot: campRoot,
		RepoPath:     repo,
	})
	if !allowed {
		if err := ctx.Err(); err != nil {
			return err
		}
		return jsoncontract.WithHint(
			camperrors.NewValidation("background", "cannot queue commit: "+string(reason), nil),
			"Remove --background to allow a synchronous commit.",
		)
	}
	return nil
}

func enqueueBackgroundCommit(cmd *cobra.Command, humanOut io.Writer, campRoot, repo string, result *commitJSONResult) error {
	ctx := cmd.Context()
	// Recheck after staging, in case it installed a hook or changed writer
	// configuration. An explicit background request must never fall back inline.
	if err := requireBackgroundCommit(ctx, campRoot, repo); err != nil {
		return err
	}
	staged, err := git.StagedFileCount(ctx, repo)
	if err != nil {
		return err
	}
	enqueued, err := defercommit.Enqueue(ctx, campRoot, repo, defercommit.EnqueueOptions{
		WriterEnv:     workitemEnvForCommit(ctx, campRoot, commitWorkitem),
		MessagePrefix: commitTagPrefix(ctx, campRoot),
	})
	if errors.Is(err, git.ErrNoChanges) {
		_, _ = fmt.Fprintln(humanOut, ui.Success("Nothing to commit"))
		return commitJSONNoop(cmd, result)
	}
	if err != nil {
		return camperrors.Wrap(err, "queue background commit")
	}
	result.Staged = staged
	_, _ = fmt.Fprintln(humanOut, ui.Success(cmdutil.DeferredCommitLine(staged)))
	_, _ = fmt.Fprintf(humanOut, "  %s · camp jobs   see it land\n", enqueued.Job.ID)
	if commitJSONOut {
		return emitBackgroundCommit(cmd.OutOrStdout(), result, backgroundQueued, enqueued)
	}
	return nil
}

func emitBackgroundCommit(out io.Writer, result *commitJSONResult, outcome backgroundCommitOutcome, enqueued *defercommit.Enqueued) error {
	receipt := backgroundCommitResult{
		SchemaVersion:         CommitBackgroundJSONVersion,
		Outcome:               outcome,
		Repo:                  result.Repo,
		Staged:                result.Staged,
		Excluded:              result.Excluded,
		ArtifactRootsDeclared: result.ArtifactRootsDeclared,
		DrainWaitedMs:         result.DrainWaitedMs,
	}
	if enqueued != nil {
		receipt.JobID = enqueued.Job.ID
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(receipt)
}
