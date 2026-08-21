package triage

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage/scaffold"
)

// InitJSONVersion is the schema version of `camp triage init --json`.
const InitJSONVersion = "triage-init/v1alpha1"

type initResult struct {
	SchemaVersion string                `json:"schema_version"`
	Files         []scaffold.FileResult `json:"files"`
	Created       []string              `json:"created"`
	Diverged      []string              `json:"diverged"`
}

func newInitCommand() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold .campaign/triage with the profile and guide",
		Long: `Write .campaign/triage/ if it is not there yet.

The profile ships with every key written out and commented, not as an empty
file inheriting invisible defaults: you should be able to read what triage
will do before you run it, and change it by deleting a line.

Nothing is ever overwritten. A file you have edited is reported as diverged
and left exactly as you wrote it: the profile is meant to be edited, so
divergence is information rather than a problem.

camp triage start does this for you on first use, so this command is only
needed when you want the files before starting a run.`,
		Args: jsoncontract.Args(InitJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Writes only the scaffold, never overwrites, calls no models",
		},
		RunE: jsoncontract.RunE(InitJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runInit(cmd, jsonOut)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(InitJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	return cmd
}

func runInit(cmd *cobra.Command, jsonOut bool) error {
	ctx := cmd.Context()

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	result, err := scaffold.Ensure(ctx, root)
	if err != nil {
		return err
	}

	out := initResult{
		SchemaVersion: InitJSONVersion,
		Files:         result.Files,
		Created:       orEmpty(result.Created()),
		Diverged:      orEmpty(result.Diverged()),
	}
	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), out)
	}
	return printScaffold(cmd.OutOrStdout(), result)
}

// printScaffold reports what the scaffold did, saying nothing when there was
// nothing to do beyond confirming the directory is present.
func printScaffold(w io.Writer, result *scaffold.Result) error {
	created := result.Created()
	if len(created) == 0 {
		if _, err := fmt.Fprintf(w, "%s is already scaffolded.\n", scaffold.DirName); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "Scaffolded %s:\n", scaffold.DirName); err != nil {
			return err
		}
		for _, path := range created {
			if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w,
			"\nRead OBEY.md next to the profile; it explains the phases and every key.\n"); err != nil {
			return err
		}
	}
	return printDivergence(w, result)
}

// printDivergence names files that differ from what camp ships.
//
// Reported, never repaired: the profile exists to be edited, and a scaffold
// that "fixed" a customized file would be the single most destructive thing
// this command could do.
func printDivergence(w io.Writer, result *scaffold.Result) error {
	diverged := result.Diverged()
	if len(diverged) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w,
		"\n%d file(s) differ from the shipped version and were left as they are:\n",
		len(diverged)); err != nil {
		return err
	}
	for _, path := range diverged {
		if _, err := fmt.Fprintf(w, "  %s\n", path); err != nil {
			return err
		}
	}
	return nil
}

// orEmpty keeps a nil slice out of the JSON contract.
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func init() {
	Cmd.AddCommand(newInitCommand())
}
