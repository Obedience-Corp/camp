package triage

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// VerifyJSONVersion is the schema version of `camp triage verify --json`.
const VerifyJSONVersion = "triage-verify/v1alpha1"

type verifyResult struct {
	SchemaVersion string                     `json:"schema_version"`
	Report        *triage.VerificationReport `json:"report"`
	ReportPath    string                     `json:"report_path"`
	DocumentPath  string                     `json:"document_path"`
	Clean         bool                       `json:"clean"`
}

func newVerifyCommand() *cobra.Command {
	var (
		jsonOut bool
		runID   string
	)

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Prove the camp matches the approved decisions",
		Long: `Check every applied row against a fresh discovery pass.

Apply without proof is just hope. Verify re-walks the camp and compares
what it finds against what each receipt says happened: a parked workitem should
carry that stage, a retired one should no longer be discoverable outside the
dungeon, a split's successors should all exist.

It reads receipts, not the plan. The plan is what was intended; the receipts
are what actually ran, and only the second one can be checked against reality.

An unexplained mismatch exits 1. That is the whole signal: the camp is not
in the state the approved decisions said it would be. A mismatch someone has
already accounted for carries an explanation and does not fail the run.

A clean verification moves the run to verified and writes verification.json
with a rendered VERIFICATION.md beside it.`,
		Args: jsoncontract.Args(VerifyJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only proof pass; writes only the run's own report",
		},
		RunE: jsoncontract.RunE(VerifyJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runVerify(cmd, jsonOut, runID)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(VerifyJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().StringVar(&runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

func runVerify(cmd *cobra.Command, jsonOut bool, runID string) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp directory")
	}
	store := triage.NewStore(root, nil)

	runID, err = resolveRunID(ctx, store, runID)
	if err != nil {
		return err
	}

	items, err := discoverAll(ctx, root, cfg)
	if err != nil {
		return err
	}

	report, err := store.Verify(ctx, triage.VerifyInput{
		RunID: runID, Items: items, Now: triage.SystemClock(),
	})
	if err != nil {
		return err
	}

	dataPath, docPath, err := store.WriteVerification(ctx, report)
	if err != nil {
		return err
	}

	result := verifyResult{
		SchemaVersion: VerifyJSONVersion,
		Report:        report,
		ReportPath:    relativeRunDir(root, dataPath),
		DocumentPath:  relativeRunDir(root, docPath),
		Clean:         report.Clean(),
	}

	if jsonOut {
		if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
			return err
		}
	} else if err := printVerify(cmd.OutOrStdout(), result); err != nil {
		return err
	}

	// Exit 1, not 2: an unexplained mismatch is a fault, not a precondition
	// the operator simply has not met yet. Something is not where the
	// approved decisions said it would be.
	if !report.Clean() {
		mismatchErr := camperrors.New("verification found " +
			plural(len(report.Unexplained()), "unexplained mismatch", "unexplained mismatches"))
		return camperrors.NewCommand(cmd.CommandPath(), 1, mismatchErr.Error(), mismatchErr)
	}
	return nil
}

// printVerify renders the human report.
func printVerify(w io.Writer, result verifyResult) error {
	report := result.Report

	if _, err := fmt.Fprintf(w, "Verified %s — %d checked, %d matched, %d mismatched\n  %s\n  %s\n",
		report.RunID, report.Totals.Checked, report.Totals.Matched, report.Totals.Mismatched,
		result.ReportPath, result.DocumentPath); err != nil {
		return err
	}

	if report.Totals.Checked == 0 {
		_, err := fmt.Fprint(w,
			"\nNothing has been applied yet, so there is nothing to prove.\n"+
				"Run camp triage apply first.\n")
		return err
	}

	for _, row := range report.Unexplained() {
		if _, err := fmt.Fprintf(w, "\n  mismatch %s\n    expected: %s\n    found:    %s\n",
			row.StableID, orNone(row.ExpectedPath, row.ExpectedStage),
			orNone(row.DiscoveredPath, row.DiscoveredStage)); err != nil {
			return err
		}
	}

	if result.Clean {
		_, err := fmt.Fprint(w,
			"\nEvery applied row is where its approved verdict said it would be.\n")
		return err
	}
	_, err := fmt.Fprint(w,
		"\nMove the workitem back and re-apply, or record why the difference is\n"+
			"expected. An unexplained mismatch keeps the run out of verified.\n")
	return err
}

// orNone renders a location and stage, or a dash when there is neither.
func orNone(path, stage string) string {
	switch {
	case path != "" && stage != "":
		return path + " (" + stage + ")"
	case path != "":
		return path
	case stage != "":
		return "stage " + stage
	default:
		return "not discoverable"
	}
}

// plural picks the right noun for a count.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func init() {
	Cmd.AddCommand(newVerifyCommand())
}
