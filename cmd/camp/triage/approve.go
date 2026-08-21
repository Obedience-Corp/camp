package triage

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// ApproveJSONVersion is the schema version of `camp triage approve --json`.
const ApproveJSONVersion = "triage-approve/v1alpha1"

type approveResult struct {
	SchemaVersion string                `json:"schema_version"`
	Approval      *triage.ApproveResult `json:"approval"`
	// Rendered reports the documents refreshed after recording, so they never
	// lag the verdicts they describe.
	Rendered *triage.RenderResult `json:"rendered,omitempty"`
}

type approveOptions struct {
	jsonOut bool
	lane    string
	batch   int
	amend   string
	reject  bool
	note    string
	runID   string
}

func newApproveCommand() *cobra.Command {
	opts := &approveOptions{}

	cmd := &cobra.Command{
		Use:   "approve [stable-id...]",
		Short: "Record verdicts on proposed dispositions",
		Long: `Record your verdict on the proposals in the active run.

This is how a decision enters. The rendered documents are output, so editing
them records nothing; approval happens here and re-rendering reflects it.

Selectors:
  camp triage approve <id> [<id>...]     name rows explicitly
  camp triage approve --lane parked      every row in a lane
  camp triage approve --batch 2          every row in a review batch

Bulk selectors deliberately do not cover terminal rows: anything that retires
a workitem into the dungeon or splits it. Approving a batch is not meaningful
consent to each irreversible action inside it, so those rows are listed and
skipped, and approving one means naming it. When you do, the confirmation
echoes the exact command apply will run.

  --amend <disposition>   approve a different disposition than proposed,
                          revalidated against the row's own vocabulary
  --reject                record a refusal; the row returns to needing a
                          proposal
  --note <text>           attach a note to the verdicts

Re-approving a verdict that already stands is reported as unchanged rather
than written twice: the stream is an argument, not a log of keystrokes.`,
		Args: jsoncontract.Args(ApproveJSONVersion, func() bool { return opts.jsonOut }, cobra.ArbitraryArgs),
		Annotations: map[string]string{
			"agent_allowed": "false",
			"agent_reason":  "Records human verdicts; approval is the operator's decision to make",
		},
		RunE: jsoncontract.RunE(ApproveJSONVersion, func() bool { return opts.jsonOut },
			func(cmd *cobra.Command, args []string) error {
				return runApprove(cmd, args, opts)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(ApproveJSONVersion, func() bool { return opts.jsonOut }))

	f := cmd.Flags()
	f.BoolVar(&opts.jsonOut, "json", false, "Output result as a single JSON object")
	f.StringVar(&opts.lane, "lane", "", "Approve every non-terminal row in a lane")
	f.IntVar(&opts.batch, "batch", 0, "Approve every non-terminal row in a review batch")
	f.StringVar(&opts.amend, "amend", "", "Approve a different disposition than the one proposed")
	f.BoolVar(&opts.reject, "reject", false, "Record a refusal instead of an approval")
	f.StringVar(&opts.note, "note", "", "Note recorded with the verdicts")
	f.StringVar(&opts.runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

func runApprove(cmd *cobra.Command, args []string, opts *approveOptions) error {
	ctx := cmd.Context()

	selector, err := buildSelector(args, opts)
	if err != nil {
		return err
	}

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	store := triage.NewStore(root, nil)

	runID, err := resolveRunID(ctx, store, opts.runID)
	if err != nil {
		return err
	}

	approval, err := store.Approve(ctx, triage.ApproveInput{
		RunID:    runID,
		Selector: selector,
		Amend:    opts.amend,
		Reject:   opts.reject,
		Note:     opts.note,
		Actor:    triage.ResolveActor(ctx),
		Now:      triage.SystemClock(),
	})
	if err != nil {
		// A selector matching nothing is a precondition failure, not a fault:
		// the operator has to pick a different selector.
		if camperrors.Is(err, camperrors.ErrNotFound) {
			return preconditionErrorFor(cmd, cmd.CommandPath(), opts.jsonOut,
				jsoncontract.WithHint(err,
					"check `camp triage review --render-only` for the lanes and rows this run has"))
		}
		return err
	}

	// Nothing recorded means the operator's intent was not achieved, whatever
	// the reason: the rows had no proposal, or a bulk selector covered only
	// terminal ones. That is a precondition failure rather than success, and
	// the detail below says which it was and what to do instead.
	if len(approval.Recorded) == 0 {
		if !opts.jsonOut {
			_ = writeApproveSkips(cmd.ErrOrStderr(), approval)
		}
		return preconditionErrorFor(cmd, cmd.CommandPath(), opts.jsonOut,
			jsoncontract.WithHint(
				camperrors.Wrap(camperrors.ErrNotFound,
					"recorded no verdicts for "+selector.Describe()),
				approveHintFor(approval)))
	}

	// Re-render in place so the documents never lag the fold. Cheap and pure,
	// so there is no reason to make the operator remember a second command.
	rendered, err := store.RenderDocuments(ctx, runID)
	if err != nil {
		return err
	}
	rendered.ReviewPath = relativeRunDir(root, rendered.ReviewPath)
	rendered.PrioritiesPath = relativeRunDir(root, rendered.PrioritiesPath)

	if opts.jsonOut {
		return writeJSON(cmd.OutOrStdout(), approveResult{
			SchemaVersion: ApproveJSONVersion,
			Approval:      approval,
			Rendered:      rendered,
		})
	}
	return writeApproveText(cmd.OutOrStdout(), approval, rendered)
}

// buildSelector turns the flags and positional args into exactly one selector.
func buildSelector(args []string, opts *approveOptions) (triage.Selector, error) {
	forms := 0
	if len(args) > 0 {
		forms++
	}
	if opts.lane != "" {
		forms++
	}
	if opts.batch > 0 {
		forms++
	}

	switch {
	case forms == 0:
		return triage.Selector{}, camperrors.NewValidation("selector",
			"name one or more stable ids, or use --lane or --batch",
			camperrors.ErrInvalidInput)
	case forms > 1:
		return triage.Selector{}, camperrors.NewValidation("selector",
			"give one selector: stable ids, --lane, or --batch",
			camperrors.ErrInvalidInput)
	}

	return triage.Selector{StableIDs: args, Lane: opts.lane, Batch: opts.batch}, nil
}

func writeApproveText(w io.Writer, approval *triage.ApproveResult, rendered *triage.RenderResult) error {
	recorded, unchanged := 0, 0
	for _, verdict := range approval.Recorded {
		if verdict.Unchanged {
			unchanged++
			continue
		}
		recorded++
	}

	if _, err := fmt.Fprintf(w, "Recorded %d verdict(s)", recorded); err != nil {
		return err
	}
	if unchanged > 0 {
		if _, err := fmt.Fprintf(w, ", %d already stood", unchanged); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\n"); err != nil {
		return err
	}

	for _, verdict := range approval.Recorded {
		marker := "  "
		if verdict.Unchanged {
			marker = "= "
		}
		if _, err := fmt.Fprintf(w, "%s%s → %s (%s)\n",
			marker, verdict.StableID, verdict.Disposition, verdict.Event); err != nil {
			return err
		}
		// A terminal verdict echoes the real mutation, so approving it is not
		// consent to a label whose meaning the operator has to remember.
		if verdict.ApplyCommand != "" && verdict.CanonicalAction.Terminal() {
			if _, err := fmt.Fprintf(w, "    apply will run: %s\n", verdict.ApplyCommand); err != nil {
				return err
			}
		}
	}

	if err := writeApproveSkips(w, approval); err != nil {
		return err
	}

	_, err := fmt.Fprintf(w, "\nRe-rendered %s\n", rendered.ReviewPath)
	return err
}

// writeApproveSkips reports what the call deliberately did not do. It runs on
// the success path and on the nothing-recorded path, because the reason a row
// was skipped is the same information either way.
func writeApproveSkips(w io.Writer, approval *triage.ApproveResult) error {
	if len(approval.SkippedTerminal) > 0 {
		if _, err := fmt.Fprintf(w,
			"\nSkipped %d terminal row(s). Approving one retires or splits a workitem,\n"+
				"so it has to be named rather than covered by a bulk selector:\n",
			len(approval.SkippedTerminal)); err != nil {
			return err
		}
		for _, id := range approval.SkippedTerminal {
			if _, err := fmt.Fprintf(w, "  %s\n", id); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "\napprove individually: camp triage approve %s\n",
			joinIDs(approval.SkippedTerminal)); err != nil {
			return err
		}
	}

	if len(approval.SkippedNoProposal) > 0 {
		if _, err := fmt.Fprintf(w, "\nSkipped %d row(s) with no proposal to approve.\n",
			len(approval.SkippedNoProposal)); err != nil {
			return err
		}
	}
	return nil
}

// approveHintFor names the next step for the reason nothing was recorded.
func approveHintFor(approval *triage.ApproveResult) string {
	if len(approval.SkippedTerminal) > 0 {
		return "approve the terminal rows by name: camp triage approve " +
			joinIDs(approval.SkippedTerminal)
	}
	if len(approval.SkippedNoProposal) > 0 {
		return "those rows need a proposal first: camp triage propose <id> --disposition <d>"
	}
	return "check `camp triage review --render-only` for the lanes and rows this run has"
}

// joinIDs renders ids for a copy-pasteable command.
func joinIDs(ids []string) string { return strings.Join(ids, " ") }

func init() {
	Cmd.AddCommand(newApproveCommand())
}
