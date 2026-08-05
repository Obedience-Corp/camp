package triage

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// ReviewJSONVersion is the schema version of `camp triage review --json`.
const ReviewJSONVersion = "triage-review/v1alpha1"

// PrioritiesJSONVersion is the schema version of `camp triage priorities --json`.
const PrioritiesJSONVersion = "triage-priorities/v1alpha1"

type reviewResult struct {
	SchemaVersion string               `json:"schema_version"`
	Render        *triage.RenderResult `json:"render"`
}

func newReviewCommand() *cobra.Command {
	var (
		jsonOut    bool
		renderOnly bool
		runID      string
	)

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Render the review documents for the active run",
		Long: `Render TRIAGE_REVIEW.md and PRIORITIES.md for the active run.

Both documents are output, not input. Verdicts are recorded with
camp triage approve; re-rendering replaces the files, so an edit made in them
is lost rather than honored. Each carries that statement at the top.

Every row in the run appears exactly once across the review's lanes, including
rows nobody has proposed anything for — a document that quietly omitted them
could be approved without the operator ever seeing what it left out.

Rendering is pure: the same run data always produces the same bytes, so
re-rendering an unchanged run is a no-op diff.`,
		Args: jsoncontract.Args(ReviewJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Deterministic render from recorded run data; writes only inside the run",
		},
		RunE: jsoncontract.RunE(ReviewJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runReview(cmd, jsonOut, renderOnly, runID)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(ReviewJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().BoolVar(&renderOnly, "render-only", false,
		"Render the documents without opening the interactive review flow")
	cmd.Flags().StringVar(&runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

func runReview(cmd *cobra.Command, jsonOut, renderOnly bool, runID string) error {
	ctx := cmd.Context()

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	store := triage.NewStore(root, nil)

	runID, err = resolveRunID(ctx, store, runID)
	if err != nil {
		return err
	}

	result, err := store.RenderDocuments(ctx, runID)
	if err != nil {
		return err
	}
	result.ReviewPath = relativeRunDir(root, result.ReviewPath)
	result.PrioritiesPath = relativeRunDir(root, result.PrioritiesPath)

	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), reviewResult{
			SchemaVersion: ReviewJSONVersion,
			Render:        result,
		})
	}

	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "Rendered %d row(s) across %d lane(s)\n  %s\n  %s\n",
		result.Rows, result.Lanes, result.ReviewPath, result.PrioritiesPath); err != nil {
		return err
	}
	// The interactive flow lands in a later sequence. Saying so is better than
	// silently doing something other than what the flag implies.
	if !renderOnly {
		if _, err := fmt.Fprintf(out,
			"\nThe interactive review flow is not built yet; this rendered the documents.\n"+
				"Record verdicts with: camp triage approve\n"); err != nil {
			return err
		}
	}
	return nil
}

type prioritiesResult struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	// ExportPath is where the copy was written; empty when the profile asks
	// for no export.
	ExportPath string `json:"export_path,omitempty"`
	Exported   bool   `json:"exported"`
	Document   string `json:"document"`
}

func newPrioritiesCommand() *cobra.Command {
	var (
		jsonOut bool
		export  bool
		runID   string
	)

	cmd := &cobra.Command{
		Use:   "priorities",
		Short: "Print the priorities brief for the active run",
		Long: `Print the priorities brief: what to work on, derived from the run's verdicts.

This is the artifact that keeps working after the session ends. The run holds
the source of truth; --export writes a rendered copy to the path the profile's
outputs.priorities_export names, so the brief lives where you already look.

The export overwrites rather than versioning. Versioned copies would recreate
the stale-priorities-document problem triage exists to end; history is git's
job. When the profile leaves the export path empty, --export reports that it
did nothing rather than failing.`,
		Args: jsoncontract.Args(PrioritiesJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only brief rendered from recorded run data",
		},
		RunE: jsoncontract.RunE(PrioritiesJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runPriorities(cmd, jsonOut, export, runID)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(PrioritiesJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().BoolVar(&export, "export", false,
		"Also write a copy to the profile's outputs.priorities_export path")
	cmd.Flags().StringVar(&runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

func runPriorities(cmd *cobra.Command, jsonOut, export bool, runID string) error {
	ctx := cmd.Context()

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	store := triage.NewStore(root, nil)

	runID, err = resolveRunID(ctx, store, runID)
	if err != nil {
		return err
	}

	in, err := triage.LoadRenderInput(ctx, store, runID)
	if err != nil {
		return err
	}
	document := string(triage.RenderPriorities(in))

	result := prioritiesResult{
		SchemaVersion: PrioritiesJSONVersion,
		RunID:         runID,
		Document:      document,
	}
	if export {
		abs, err := store.ExportPriorities(ctx, root, runID)
		if err != nil {
			return err
		}
		if abs != "" {
			result.ExportPath = relativeRunDir(root, abs)
			result.Exported = true
		}
	}

	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), result)
	}

	out := cmd.OutOrStdout()
	if _, err := fmt.Fprint(out, document); err != nil {
		return err
	}
	if export {
		if result.Exported {
			_, err = fmt.Fprintf(out, "\nExported to %s\n", result.ExportPath)
		} else {
			_, err = fmt.Fprintf(out,
				"\nNo export path set. Set outputs.priorities_export in the profile to keep\n"+
					"a copy where you already look; the run's own copy always exists.\n")
		}
		return err
	}
	return nil
}

func init() {
	Cmd.AddCommand(newReviewCommand(), newPrioritiesCommand())
}
