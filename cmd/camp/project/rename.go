package project

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	projectlinked "github.com/Obedience-Corp/camp/cmd/camp/project/linked"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	projectrename "github.com/Obedience-Corp/camp/internal/project/rename"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

const ProjectRenameJSONVersion = "project-rename/v1alpha1"

type projectRenameFlags struct {
	remoteURL string
	noVerify  bool
	dryRun    bool
	noCommit  bool
	campaign  string
	json      bool
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
		Use:     "rename <current> <new>",
		Aliases: []string{"mv"},
		Short:   "Rename a managed project",
		Long: `Rename a managed project and migrate its active Camp references.

Supported projects are declared Git submodules, linked workspace symlinks,
and ordinary campaign-owned directories tracked by the campaign repository.
Dirty project checkouts and linked worktrees are preserved. Destination
collisions and unmanaged directories are rejected before mutation.

Camp never guesses that an upstream repository was renamed. Pass --remote-url
to change origin explicitly as part of the same transaction.

Examples:
  camp project rename api-old api
  camp project mv api-old api
  camp project rename obey-installer festival-installer \
    --remote-url git@github.com:Obedience-Corp/festival-installer.git
  camp project rename api-old api --dry-run --json`,
		Args: jsoncontract.Args(ProjectRenameJSONVersion, func() bool { return flags.json }, cobra.ExactArgs(2)),
	}
	cmd.RunE = jsoncontract.RunE(ProjectRenameJSONVersion, func() bool { return flags.json }, func(cmd *cobra.Command, args []string) error {
		return runProjectRename(cmd, args, flags)
	})
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(ProjectRenameJSONVersion, func() bool { return flags.json }))

	cmd.Flags().StringVar(&flags.remoteURL, "remote-url", "", "Explicitly update the project's origin URL")
	cmd.Flags().BoolVar(&flags.noVerify, "no-verify", false, "Skip remote connectivity verification")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "Print the complete plan without writing")
	cmd.Flags().BoolVar(&flags.noCommit, "no-commit", false, "Apply the rename without a campaign commit")
	cmd.Flags().StringVarP(&flags.campaign, "campaign", "c", "", "Target campaign by name or ID; omit value to pick interactively")
	cmd.Flags().BoolVar(&flags.json, "json", false, "Output a versioned JSON plan or result")
	cmd.Flags().Lookup("campaign").NoOptDefVal = projectlinked.NoOptCampaign
	return cmd
}

func runProjectRename(cmd *cobra.Command, args []string, flags projectRenameFlags) error {
	ctx := cmd.Context()
	resolver := newProjectCampaignResolver(cmd.ErrOrStderr(), "camp project rename --campaign <name> <current> <new>")
	cfg, root, err := resolver.Resolve(ctx, flags.campaign, cmd.Flags().Changed("campaign"))
	if err != nil {
		return err
	}
	opts := projectrename.Options{
		RemoteURL: flags.remoteURL, VerifyRemote: !flags.noVerify, DryRun: flags.dryRun,
	}
	plan, err := projectrename.Plan(ctx, root, args[0], args[1], opts)
	if err != nil {
		return err
	}

	result := &projectrename.Result{Plan: plan}
	if !flags.dryRun {
		result, err = projectrename.Apply(ctx, plan, opts)
		if err != nil {
			return err
		}
	}

	var commitResult *projectRenameCommit
	if !flags.dryRun && !flags.noCommit && plan.AutoCommitEligible {
		campaignID := ""
		if cfg != nil {
			campaignID = cfg.ID
		}
		outcome := commit.Project(ctx, commit.ProjectOptions{
			Options: commit.Options{
				CampaignRoot: root, CampaignID: campaignID,
				Files: commit.NormalizeFiles(root, plan.CommitFiles...), SelectiveOnly: true,
			},
			Action:      commit.ProjectRename,
			ProjectName: plan.OldName + " -> " + plan.NewName,
		})
		commitResult = &projectRenameCommit{
			Committed: outcome.Committed, Deferred: outcome.Deferred,
			Skipped: outcome.Skipped, Message: outcome.Message,
		}
		if outcome.Err != nil {
			commitResult.Error = outcome.Err.Error()
			result.Warnings = append(result.Warnings, "automatic commit failed: "+outcome.Err.Error())
		}
	} else if !flags.dryRun && !flags.noCommit && !plan.AutoCommitEligible {
		commitResult = &projectRenameCommit{Skipped: true, Message: plan.AutoCommitSkipReason}
		result.Warnings = append(result.Warnings, plan.AutoCommitSkipReason)
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
	undo := "camp project rename " + p.NewName + " " + p.OldName
	if p.OldURL != p.NewURL && p.OldURL != "" {
		undo += " --remote-url " + p.OldURL
	}
	return writef("\nUndo: %s\n", strings.TrimSpace(undo))
}
