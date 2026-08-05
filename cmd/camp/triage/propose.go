package triage

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// ProposeJSONVersion is the schema version of `camp triage propose --json`.
const ProposeJSONVersion = "triage-propose/v1alpha1"

type proposeResult struct {
	SchemaVersion string                `json:"schema_version"`
	RunID         string                `json:"run_id"`
	Proposal      *triage.ProposeResult `json:"proposal"`
}

func newProposeCommand() *cobra.Command {
	var (
		jsonOut     bool
		disposition string
		file        string
		summary     string
		confidence  string
		runID       string
	)

	cmd := &cobra.Command{
		Use:   "propose <stable-id>",
		Short: "Propose a disposition for a row",
		Long: `Propose what should happen to one row.

The disposition is a label from the row's type vocabulary; camp resolves it to
the action it will actually perform and records both. That indirection is what
lets a campaign rename its labels without triage learning a new mutation.

A proposal is not a decision. Terminal actions - dungeon moves and splits -
always require a human to approve them, and the result says so.

One proposal is live per row, but nothing is overwritten: proposing again
retires the previous one with a superseded event and records the new one, so
the stream keeps the whole argument rather than only where it landed.

The rationale can come from a file or from --summary for a one-liner:
  camp triage propose <id> --disposition completed --summary "shipped in #239"
  camp triage propose <id> --disposition consolidate --file rationale.json`,
		Args: jsoncontract.Args(ProposeJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Records an advisory proposal; terminal actions still require human approval",
		},
		RunE: jsoncontract.RunE(ProposeJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, args []string) error {
				return runPropose(cmd, args[0], proposeOptions{
					disposition: disposition,
					file:        file,
					summary:     summary,
					confidence:  confidence,
					jsonOut:     jsonOut,
					runID:       runID,
				})
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(ProposeJSONVersion, func() bool { return jsonOut }))

	f := cmd.Flags()
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	f.StringVar(&disposition, "disposition", "", "Disposition label from the row's type vocabulary")
	f.StringVar(&file, "file", "", "Path to the rationale JSON ('-' for stdin)")
	f.StringVar(&summary, "summary", "", "One-line rationale, instead of --file")
	f.StringVar(&confidence, "confidence", string(triage.ConfidenceMedium),
		"Confidence in the proposal when using --summary: high, medium, or low")
	f.StringVar(&runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

type proposeOptions struct {
	disposition string
	file        string
	summary     string
	confidence  string
	jsonOut     bool
	runID       string
}

func runPropose(cmd *cobra.Command, stableID string, opts proposeOptions) error {
	ctx := cmd.Context()

	if opts.file == "" && opts.summary == "" {
		return camperrors.NewValidation("rationale",
			"give --summary for a one-liner or --file for a full rationale",
			camperrors.ErrInvalidInput)
	}
	if opts.file != "" && opts.summary != "" {
		return camperrors.NewValidation("rationale",
			"--file and --summary are alternatives; give one",
			camperrors.ErrInvalidInput)
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

	rationale, err := readRationale(cmd, opts)
	if err != nil {
		return err
	}

	result, err := store.Propose(ctx, triage.ProposeInput{
		RunID:       runID,
		StableID:    stableID,
		Disposition: opts.disposition,
		Rationale:   rationale,
		Actor:       gitIdentity(cmd, root),
		Now:         triage.SystemClock(),
	})
	if err != nil {
		return err
	}

	if opts.jsonOut {
		return writeJSON(cmd.OutOrStdout(), proposeResult{
			SchemaVersion: ProposeJSONVersion,
			RunID:         runID,
			Proposal:      result,
		})
	}
	return writeProposeText(cmd.OutOrStdout(), result)
}

// readRationale builds the rationale from a file or from --summary.
func readRationale(cmd *cobra.Command, opts proposeOptions) (*triage.Rationale, error) {
	if opts.summary != "" {
		return &triage.Rationale{
			SchemaVersion: triage.SchemaVersion,
			Summary:       opts.summary,
			Confidence:    triage.Confidence(opts.confidence),
		}, nil
	}

	var (
		raw []byte
		err error
	)
	if opts.file == "-" {
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(opts.file)
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "read rationale %s", opts.file)
	}

	var rationale triage.Rationale
	if err := triage.ParseDocument(raw, &rationale, triage.Strict); err != nil {
		return nil, err
	}
	return &rationale, nil
}

// gitIdentity resolves who is recording the verdict.
//
// Verdicts are attributed because the whole model is recorded judgment: a
// decision nobody is named on cannot be questioned later. An unconfigured git
// falls back to a placeholder rather than blocking the proposal, since losing
// the attribution is better than losing the judgment.
func gitIdentity(cmd *cobra.Command, root string) string {
	out, err := exec.CommandContext(cmd.Context(), "git", "-C", root, "config", "user.name").Output()
	if name := strings.TrimSpace(string(out)); err == nil && name != "" {
		return name
	}
	return "unknown"
}

func writeProposeText(w io.Writer, result *triage.ProposeResult) error {
	if result.Superseded != "" {
		if _, err := fmt.Fprintf(w, "superseded the previous proposal (%s)\n", result.Superseded); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "proposed %s for %s\n  action: %s\n",
		result.Disposition, result.StableID, result.CanonicalAction); err != nil {
		return err
	}
	if result.RequiresApproval {
		if _, err := fmt.Fprintf(w,
			"  this is a terminal action and will not run until you approve it\n"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "  rationale: %s\n", result.RationaleRef)
	return err
}

func init() {
	Cmd.AddCommand(newProposeCommand())
}
