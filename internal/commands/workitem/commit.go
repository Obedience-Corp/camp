package workitem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// noContextHint is the multi-line refusal message printed when the resolver
// returns no workitem.
const noContextHint = `no workitem context resolved from cwd

Try one of:
  --workitem <selector>            explicit workitem to scope this commit
  cd workflow/<type>/<slug> && ...  run from inside a workitem directory
  camp workitem current <selector>  set a session-wide default`

func newCommitCommand() *cobra.Command {
	var (
		flagMessage                 []string
		flagWorkitem                string
		flagProject                 string
		flagFestival                string
		flagIncludes                []string
		flagExcludes                []string
		flagStaged                  bool
		flagIncludeSubmodulePointer bool
		flagDryRun                  bool
		flagJSON                    bool
	)

	cmd := &cobra.Command{
		Use:   "commit [selector]",
		Short: "Commit changes scoped to a workitem",
		Long: `Stage and commit changes belonging to a resolved workitem.

The staging plan is computed from the resolver context (cwd-aware, with
explicit positional <selector> or --project overrides) and printed to stderr
before the commit runs. The plan never silently widens to "git add ." at the
campaign root.

See docs/workitem-commit-reference.md for the staging matrix and flag
precedence.`,
		Args: jsoncontract.Args(WorkitemCommitJSONVersion, func() bool { return flagJSON }, cobra.MaximumNArgs(1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Scoped commit command; honors --json and --dry-run for automation",
		},
		RunE: jsoncontract.RunE(WorkitemCommitJSONVersion, func() bool { return flagJSON }, func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			selector := flagWorkitem
			if selector == "" && len(args) == 1 {
				selector = args[0]
			}
			return runCommit(ctx, cmd, commitFlags{
				Message:                 commitkit.JoinMessages(flagMessage),
				Workitem:                selector,
				Project:                 flagProject,
				Festival:                flagFestival,
				Includes:                flagIncludes,
				Excludes:                flagExcludes,
				Staged:                  flagStaged,
				IncludeSubmodulePointer: flagIncludeSubmodulePointer,
				DryRun:                  flagDryRun,
				JSON:                    flagJSON,
			})
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemCommitJSONVersion, func() bool { return flagJSON }))
	cmd.Flags().StringArrayVarP(&flagMessage, "message", "m", nil, "commit message (repeatable; multiple -m are joined git-style into subject + body; required unless --dry-run)")
	cmd.Flags().StringVar(&flagWorkitem, "workitem", "", "explicit workitem selector (overrides cwd-based resolution)")
	cmd.Flags().StringVar(&flagProject, "project", "", "force project-repo context by name (skips resolver)")
	cmd.Flags().StringVar(&flagFestival, "festival", "", "festival id for the festival resolver tier")
	cmd.Flags().StringArrayVar(&flagIncludes, "include", nil, "additional path to stage (repeatable; relative to repo root)")
	cmd.Flags().StringArrayVar(&flagExcludes, "exclude", nil, "path to remove from the staging plan (repeatable)")
	cmd.Flags().BoolVar(&flagStaged, "staged", false, "commit whatever is already in the git index")
	cmd.Flags().BoolVar(&flagIncludeSubmodulePointer, "include-submodule-pointer", false, "include dirty project submodule pointers in the plan")
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "print the staging plan and exit without committing")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit the staging plan and commit result as JSON on stdout")
	return cmd
}

type commitFlags struct {
	Message                 string
	Workitem                string
	Project                 string
	Festival                string
	Includes                []string
	Excludes                []string
	Staged                  bool
	IncludeSubmodulePointer bool
	DryRun                  bool
	JSON                    bool
}

func runCommit(ctx context.Context, cmd *cobra.Command, flags commitFlags) error {
	if flags.Message == "" && !flags.DryRun {
		return camperrors.NewValidation("message", "commit message required (use -m \"<msg>\")", nil)
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	plan, err := ComputePlan(ctx, campaignRoot, PlanOptions{
		Explicit:                flags.Workitem,
		Project:                 flags.Project,
		FestivalID:              flags.Festival,
		Includes:                flags.Includes,
		Excludes:                flags.Excludes,
		StagedOnly:              flags.Staged,
		IncludeSubmodulePointer: flags.IncludeSubmodulePointer,
		CampaignID:              cfg.ID,
		CampaignName:            cfg.Name,
	})
	if err != nil {
		if errors.Is(err, ErrNoWorkitemContext) {
			if flags.JSON {
				return jsoncontract.WithHint(
					camperrors.NewValidation("workitem", "no workitem context resolved from cwd", err),
					noContextHint,
				)
			}
			if _, writeErr := fmt.Fprintln(cmd.ErrOrStderr(), noContextHint); writeErr != nil {
				return writeErr
			}
			return camperrors.NewCommand("camp workitem commit", 2, "", err)
		}
		return err
	}

	for _, w := range plan.Warnings {
		if _, writeErr := fmt.Fprintln(cmd.ErrOrStderr(), "warning: "+w); writeErr != nil {
			return writeErr
		}
	}

	if !flags.Staged && !flags.DryRun {
		if err := assertCleanIndex(ctx, plan.RepoRoot); err != nil {
			return err
		}
	}

	if !flags.JSON {
		if perr := PrintPlan(cmd.ErrOrStderr(), plan); perr != nil {
			return perr
		}
	}

	if len(plan.Stage) == 0 && len(plan.PreStaged) == 0 {
		return camperrors.NewValidation("plan",
			"empty staging plan; pass --include <path> or run from inside a changed workitem", nil)
	}

	if flags.DryRun {
		if flags.JSON {
			return emitJSON(cmd.OutOrStdout(), plan, "")
		}
		return nil
	}

	res := commit.Workitem(ctx, commit.WorkitemOptions{
		Options: commit.Options{
			CampaignRoot:  plan.RepoRoot,
			CampaignID:    cfg.ID,
			CampaignName:  cfg.Name,
			Files:         plan.Stage,
			PreStaged:     plan.PreStaged,
			SelectiveOnly: true,
		},
		Action:      commit.WorkitemScope,
		WorkitemID:  plan.Workitem.StableID,
		WorkitemRef: plan.WorkitemRef,
		QuestID:     plan.QuestID,
		FestivalRef: plan.FestivalRef,
		Title:       flags.Message,
	})
	if res.Err != nil {
		return camperrors.Wrap(res.Err, "commit workitem")
	}
	if res.NoChanges {
		if flags.JSON {
			return emitJSON(cmd.OutOrStdout(), plan, "")
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), res.Message)
		return err
	}

	sha, shaErr := lastCommitSHA(ctx, plan.RepoRoot)
	if shaErr != nil {
		if _, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not read last commit SHA: %v\n", shaErr); writeErr != nil {
			return writeErr
		}
	}
	if sha != "" {
		// commit.Workitem composes the tagged subject internally and never
		// returns it, so the landed subject is read back from git rather than
		// reusing flags.Message (untagged): every other CommitEvidence call
		// site records the actual tagged git subject, and readers like
		// `workitem commits` parse the campaign tag back out of it.
		subject := flags.Message
		if s, serr := lastCommitSubject(ctx, plan.RepoRoot, sha); serr == nil {
			subject = s
		}
		ledger.NewFromRoot(ctx, campaignRoot, ledger.WarnTo(cmd.ErrOrStderr())).
			CommitEvidence(ctx,
				ledgerkit.Scope{Workitem: plan.WorkitemRef, Festival: plan.FestivalRef, Quest: plan.QuestID},
				campaignRoot, plan.RepoRoot, sha, subject)
	}
	if flags.JSON {
		return emitJSON(cmd.OutOrStdout(), plan, sha)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "committed %s\n", sha)
	return err
}

// WorkitemCommitJSONVersion is declared in json_contract.go alongside the
// rest of the agent-facing JSON schema versions so the seq-06 contract is
// authored in one place.

func emitJSON(w writeFlusher, plan *StagingPlan, sha string) error {
	stage := plan.Stage
	if stage == nil {
		stage = []string{}
	}
	payload := struct {
		SchemaVersion string         `json:"schema_version"`
		Workitem      string         `json:"workitem"`
		Ref           string         `json:"workitem_ref,omitempty"`
		QuestID       string         `json:"quest_id,omitempty"`
		FestivalRef   string         `json:"festival_ref,omitempty"`
		Tag           string         `json:"tag"`
		Context       PlanContext    `json:"context"`
		ContextNote   string         `json:"context_note,omitempty"`
		RepoRoot      string         `json:"repo_root"`
		Stage         []string       `json:"stage"`
		PreStaged     []string       `json:"pre_staged,omitempty"`
		Skip          []SkippedEntry `json:"skip,omitempty"`
		SHA           string         `json:"sha,omitempty"`
		Warnings      []string       `json:"warnings,omitempty"`
	}{
		SchemaVersion: WorkitemCommitJSONVersion,
		Workitem:      plan.Workitem.StableID,
		Ref:           plan.WorkitemRef,
		QuestID:       plan.QuestID,
		FestivalRef:   plan.FestivalRef,
		Tag:           plan.Tag,
		Context:       plan.Context,
		ContextNote:   plan.ContextNote,
		RepoRoot:      plan.RepoRoot,
		Stage:         stage,
		PreStaged:     plan.PreStaged,
		Skip:          plan.Skip,
		SHA:           sha,
		Warnings:      plan.Warnings,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

type writeFlusher interface {
	Write([]byte) (int, error)
}

func lastCommitSHA(ctx context.Context, repoRoot string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func lastCommitSubject(ctx context.Context, repoRoot, sha string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "log", "-1", "--format=%s", sha).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
