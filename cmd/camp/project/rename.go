package project

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	projectlinked "github.com/Obedience-Corp/camp/cmd/camp/project/linked"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	projectrename "github.com/Obedience-Corp/camp/internal/project/rename"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

const ProjectRenameJSONVersion = "project-rename/v1alpha1"

type projectRenameFlags struct {
	remoteURL   string
	noVerify    bool
	dryRun      bool
	noCommit    bool
	yes         bool
	interactive bool
	// synchronous waits for the git commit. The review screen is already
	// blocking on the user's confirmation, so the result it shows is final.
	synchronous bool
	campaign    string
	json        bool
}

type projectRenameEnvelope struct {
	SchemaVersion string                `json:"schema_version"`
	DryRun        bool                  `json:"dry_run"`
	Result        *projectrename.Result `json:"result"`
	Commit        *projectRenameCommit  `json:"commit,omitempty"`
}

type projectRenameCommit struct {
	Committed bool   `json:"committed"`
	Deferred  bool   `json:"deferred"`
	Skipped   bool   `json:"skipped"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

func newProjectRenameCommand() *cobra.Command {
	var flags projectRenameFlags
	cmd := &cobra.Command{
		Use:     "rename [current] [new]",
		Aliases: []string{"mv"},
		Short:   "Rename a managed project",
		Long: `Rename a managed project and migrate its active Camp references.

Supported projects are declared Git submodules, linked workspace symlinks,
and ordinary camp-owned directories tracked by the camp repository.
Dirty project checkouts and linked worktrees are preserved. Destination
collisions and unmanaged directories are rejected before mutation.

In a terminal, with neither --json nor --yes, the command opens a review
before it writes. Choose the project, type the new name, and confirm the
plan. Pass both names with --yes, or run the command without a terminal,
to apply immediately. --json keeps the scripted plan and result.

Camp never guesses that an upstream repository was renamed. Pass --remote-url
to change origin explicitly as part of the same transaction. Inside the
review, u edits that URL before you confirm.

Examples:
  camp project rename
  camp project rename api-old api
  camp project rename api-old api --yes
  camp project mv api-old api
  camp project rename obey-installer festival-installer \
    --remote-url git@github.com:Obedience-Corp/festival-installer.git
  camp project rename api-old api --dry-run --json`,
		Args: jsoncontract.Args(ProjectRenameJSONVersion, func() bool { return flags.json }, projectRenameArgs(&flags)),
	}
	cmd.RunE = jsoncontract.RunE(ProjectRenameJSONVersion, func() bool { return flags.json }, func(cmd *cobra.Command, args []string) error {
		return runProjectRename(cmd, args, flags)
	})
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(ProjectRenameJSONVersion, func() bool { return flags.json }))

	cmd.Flags().StringVar(&flags.remoteURL, "remote-url", "", "Explicitly update the project's origin URL")
	cmd.Flags().BoolVar(&flags.noVerify, "no-verify", false, "Skip remote connectivity verification")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "Print the complete plan without writing")
	cmd.Flags().BoolVar(&flags.noCommit, "no-commit", false, "Apply the rename without a camp commit")
	cmd.Flags().BoolVar(&flags.yes, "yes", false, "Apply immediately without the review screen")
	cmd.Flags().BoolVarP(&flags.interactive, "interactive", "i", false, "Open the review screen")
	cmd.Flags().StringVarP(&flags.campaign, "campaign", "c", "", "Target camp by name or ID; omit value to pick interactively")
	cmd.Flags().BoolVar(&flags.json, "json", false, "Output a versioned JSON plan or result")
	cmd.Flags().Lookup("campaign").NoOptDefVal = projectlinked.NoOptCampaign
	return cmd
}

func projectRenameArgs(flags *projectRenameFlags) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if projectRenameUsesTUI(*flags, stdoutIsTTY()) {
			if len(args) > 2 {
				return fmt.Errorf("accepts at most 2 arg(s), received %d", len(args))
			}
			return nil
		}
		return cobra.ExactArgs(2)(cmd, args)
	}
}

// projectRenameUsesTUI reports whether this invocation should open the review
// screen. --json and --yes stay on the scripted path. -i forces the screen.
// Otherwise an interactive terminal opens it.
func projectRenameUsesTUI(flags projectRenameFlags, isTTY bool) bool {
	if flags.json || flags.yes {
		return false
	}
	if flags.interactive {
		return true
	}
	return isTTY
}

func runProjectRename(cmd *cobra.Command, args []string, flags projectRenameFlags) error {
	if projectRenameUsesTUI(flags, stdoutIsTTY()) {
		if !stdoutIsTTY() {
			return camperrors.New("project rename review needs a terminal; pass both names to apply without one")
		}
		return runProjectRenameTUI(cmd, args, flags)
	}
	if len(args) != 2 {
		return cobra.ExactArgs(2)(cmd, args)
	}

	ctx := cmd.Context()
	resolver := newProjectCampaignResolver(cmd.ErrOrStderr(), "camp project rename --campaign <name> <current> <new>")
	cfg, root, err := resolver.Resolve(ctx, flags.campaign, cmd.Flags().Changed("campaign"))
	if err != nil {
		return err
	}
	campaignID := ""
	if cfg != nil {
		campaignID = cfg.ID
	}
	opts := projectrename.Options{
		RemoteURL: flags.remoteURL, VerifyRemote: !flags.noVerify, DryRun: flags.dryRun,
	}
	plan, err := projectrename.Plan(ctx, root, args[0], args[1], opts)
	if err != nil {
		return err
	}
	result, commitResult, err := finishProjectRename(ctx, campaignID, root, plan, flags.remoteURL, flags)
	if err != nil {
		return err
	}
	if flags.json {
		return writeProjectRenameJSON(cmd.OutOrStdout(), projectRenameEnvelope{
			SchemaVersion: ProjectRenameJSONVersion,
			DryRun:        flags.dryRun,
			Result:        result,
			Commit:        commitResult,
		})
	}
	return printProjectRename(cmd.OutOrStdout(), result, flags.dryRun, flags.noCommit, commitResult)
}

// finishProjectRename applies a planned rename and records the camp commit
// outcome. Dry-run returns the plan unchanged.
func finishProjectRename(ctx context.Context, campaignID, root string, plan *projectrename.PlanResult, remoteURL string, flags projectRenameFlags) (*projectrename.Result, *projectRenameCommit, error) {
	result := &projectrename.Result{Plan: plan}
	if flags.dryRun {
		return result, nil, nil
	}
	opts := projectrename.Options{RemoteURL: remoteURL, VerifyRemote: !flags.noVerify}
	result, err := projectrename.Apply(ctx, plan, opts)
	if err != nil {
		return result, nil, err
	}
	plan, err = appliedProjectRenamePlan(result)
	if err != nil {
		return result, nil, err
	}
	return result, commitProjectRename(ctx, campaignID, root, plan, result, flags), nil
}

func commitProjectRename(ctx context.Context, campaignID, root string, plan *projectrename.PlanResult, result *projectrename.Result, flags projectRenameFlags) *projectRenameCommit {
	if flags.noCommit || plan == nil {
		return nil
	}
	if !plan.AutoCommitEligible {
		result.Warnings = append(result.Warnings, plan.AutoCommitSkipReason)
		return &projectRenameCommit{Skipped: true, Message: plan.AutoCommitSkipReason}
	}
	outcome := commit.Project(ctx, commit.ProjectOptions{
		Options: commit.Options{
			CampaignRoot: root, CampaignID: campaignID,
			Files: commit.NormalizeFiles(root, plan.CommitFiles...), SelectiveOnly: true,
			Synchronous: flags.synchronous,
		},
		Action:      commit.ProjectRename,
		ProjectName: plan.OldName + " -> " + plan.NewName,
	})
	commitResult := &projectRenameCommit{
		Committed: outcome.Committed, Deferred: outcome.Deferred,
		Skipped: outcome.Skipped, Message: outcome.Message,
	}
	if outcome.Err != nil {
		commitResult.Error = outcome.Err.Error()
		result.Warnings = append(result.Warnings, "automatic commit failed: "+outcome.Err.Error())
	}
	return commitResult
}

// appliedProjectRenamePlan makes Apply's refreshed plan authoritative for all
// post-mutation behavior. Apply deliberately re-plans immediately before
// mutation, so its eligibility and commit file set may be newer than the
// command's initial dry-run-capable plan.
func appliedProjectRenamePlan(result *projectrename.Result) (*projectrename.PlanResult, error) {
	if result == nil || result.Plan == nil {
		return nil, camperrors.New("project rename apply returned no refreshed plan")
	}
	return result.Plan, nil
}

func writeProjectRenameJSON(w io.Writer, envelope projectRenameEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

func printProjectRename(w io.Writer, result *projectrename.Result, dryRun, noCommit bool, committed *projectRenameCommit) error {
	p := result.Plan
	heading := "Project rename plan"
	if !dryRun {
		heading = "Project renamed"
	}
	writef := func(format string, args ...any) error {
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	if err := writef("%s %s\n\n", ui.SuccessIcon(), ui.Success(heading)); err != nil {
		return err
	}
	if err := writef("  Project:   %s -> %s\n", p.OldName, p.NewName); err != nil {
		return err
	}
	if err := writef("  Kind:      %s\n", p.Kind); err != nil {
		return err
	}
	if err := writef("  Path:      %s -> %s\n", p.OldPath, p.NewPath); err != nil {
		return err
	}
	if p.OldURL != "" || p.NewURL != "" {
		if p.OldURL == p.NewURL {
			if err := writef("  Remote:    %s (unchanged)\n", p.OldURL); err != nil {
				return err
			}
		} else {
			if err := writef("  Remote:    %s -> %s\n", p.OldURL, p.NewURL); err != nil {
				return err
			}
		}
	}
	moved := 0
	for _, change := range p.Worktrees {
		if change.Moved {
			moved++
		}
	}
	records := 0
	for _, change := range p.Metadata {
		records += change.Records
	}
	if err := writef("  Worktrees: %d moved; %d retained externally\n", moved, len(p.Worktrees)-moved); err != nil {
		return err
	}
	if err := writef("  Metadata:  %d stores; %d records\n", len(p.Metadata), records); err != nil {
		return err
	}

	if dryRun {
		_, err := fmt.Fprintln(w, "\nNo changes made.")
		return err
	}
	for _, warning := range result.Warnings {
		if err := writef("  %s %s\n", ui.WarningIcon(), warning); err != nil {
			return err
		}
	}
	if noCommit {
		if _, err := fmt.Fprintln(w, "\n  Commit: skipped (--no-commit)"); err != nil {
			return err
		}
	} else if committed != nil && committed.Message != "" {
		if err := writef("\n  Commit: %s\n", committed.Message); err != nil {
			return err
		}
	}
	if len(result.ResidualReferences) > 0 {
		if err := writef("  Historical: %d tracked references retained (run git grep -n -- %s for all)\n",
			len(result.ResidualReferences), p.OldName); err != nil {
			return err
		}
	}
	return writef("\nUndo: %s\n", projectRenameUndo(p))
}

func projectRenameUndo(p *projectrename.PlanResult) string {
	if p == nil {
		return ""
	}
	undo := "camp project rename " + p.NewName + " " + p.OldName
	if p.OldURL != p.NewURL && p.OldURL != "" {
		undo += " --remote-url " + p.OldURL
	}
	return strings.TrimSpace(undo)
}
