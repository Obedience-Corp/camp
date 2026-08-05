package triage

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/triage"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// EvidenceJSONVersion is the schema version of `camp triage evidence --json`.
const EvidenceJSONVersion = "triage-evidence/v1alpha1"

type evidenceSetResult struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	StableID      string `json:"stable_id"`
	// Written is false when an identical record was already stored, so a
	// driver retrying a batch can tell a no-op from a change.
	Written    bool   `json:"written"`
	NoEvidence bool   `json:"no_evidence"`
	Path       string `json:"path"`
}

func newEvidenceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evidence",
		Short: "Submit or draft evidence for a row",
		Long: `Submit or draft the evidence record for one row of the active run.

Evidence is advisory data, never authority. Camp validates the record against
the triage/v1alpha1 schema and stores it; what it means for the row is decided
by a proposal and then by a human approving one.

Commands:
  set        Store a record from a file, or mark a row decided without one
  template   Print a record with the facts camp already knows filled in`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newEvidenceSetCommand(), newEvidenceTemplateCommand())
	return cmd
}

func newEvidenceSetCommand() *cobra.Command {
	var (
		jsonOut    bool
		file       string
		noEvidence bool
		runID      string
	)

	cmd := &cobra.Command{
		Use:   "set <stable-id>",
		Short: "Store an evidence record for a row",
		Long: `Store the evidence record for one row.

The record is validated against the triage/v1alpha1 schema before anything is
written. A rejection lists every violated field with its allowed values, so one
submission produces one complete list of what to fix rather than a sequence of
one-at-a-time failures.

Storing is idempotent by content: resubmitting an identical record changes
nothing, so a driver that retries a batch does not churn the run.

--no-evidence records that the row was judged without a gathered record. That
is a real answer, not a missing one: it satisfies the same requirement a full
record does, while stating honestly that no reading was done.`,
		Args: jsoncontract.Args(EvidenceJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Validated evidence submission from an explicit file; camp is the only writer",
		},
		RunE: jsoncontract.RunE(EvidenceJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, args []string) error {
				return runEvidenceSet(cmd, args[0], file, noEvidence, jsonOut, runID)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(EvidenceJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().StringVar(&file, "file", "", "Path to the evidence record JSON ('-' for stdin)")
	cmd.Flags().BoolVar(&noEvidence, "no-evidence", false,
		"Record that the row was judged without a gathered record")
	cmd.Flags().StringVar(&runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

func runEvidenceSet(cmd *cobra.Command, stableID, file string, noEvidence, jsonOut bool, runID string) error {
	ctx := cmd.Context()

	if file == "" && !noEvidence {
		return camperrors.NewValidation("file",
			"is required unless --no-evidence is given", camperrors.ErrInvalidInput)
	}
	if file != "" && noEvidence {
		return camperrors.NewValidation("file",
			"cannot be combined with --no-evidence: a no-evidence row has no record to read",
			camperrors.ErrInvalidInput)
	}

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	store := triage.NewStore(root, nil)

	runID, err = resolveRunID(ctx, store, runID)
	if err != nil {
		return err
	}
	row, err := findRow(ctx, store, runID, stableID)
	if err != nil {
		return err
	}

	var record *triage.EvidenceRecord
	if noEvidence {
		record = &triage.EvidenceRecord{
			SchemaVersion: triage.SchemaVersion,
			StableID:      row.StableID,
			NoEvidence:    true,
			ProducedBy: triage.ProducedBy{
				Role:    triage.EvidenceRoleHuman,
				Runtime: "camp triage evidence set --no-evidence",
				At:      triage.SystemClock(),
			},
		}
	} else {
		record, err = readEvidenceFile(cmd, file)
		if err != nil {
			return err
		}
		// The id in the record must match the row it is being filed under, or
		// a copy-pasted record would silently overwrite the wrong row.
		if record.StableID != "" && record.StableID != row.StableID {
			return camperrors.NewValidation("stable_id",
				"record is for "+record.StableID+" but was submitted for "+row.StableID,
				camperrors.ErrInvalidInput)
		}
		record.StableID = row.StableID
	}

	written, err := store.WriteEvidence(ctx, runID, record)
	if err != nil {
		return err
	}

	result := evidenceSetResult{
		SchemaVersion: EvidenceJSONVersion,
		RunID:         runID,
		StableID:      row.StableID,
		Written:       written,
		NoEvidence:    record.NoEvidence,
		Path:          relativeRunDir(root, store.EvidencePath(runID, row.StableID)),
	}
	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), result)
	}

	verb := "stored"
	if !written {
		verb = "unchanged (identical record already stored)"
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "evidence %s for %s\n  %s\n", verb, row.StableID, result.Path)
	return err
}

// readEvidenceFile parses a submitted record strictly: unknown fields are
// rejected, because a typo in an agent-produced record would otherwise be
// silently dropped and the row would look reviewed when part of the reading
// never landed.
func readEvidenceFile(cmd *cobra.Command, file string) (*triage.EvidenceRecord, error) {
	var (
		raw []byte
		err error
	)
	if file == "-" {
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(file)
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "read evidence record %s", file)
	}

	var record triage.EvidenceRecord
	if err := triage.ParseDocument(raw, &record, triage.Strict); err != nil {
		return nil, err
	}
	return &record, nil
}

func newEvidenceTemplateCommand() *cobra.Command {
	var runID string

	cmd := &cobra.Command{
		Use:   "template <stable-id>",
		Short: "Print an evidence record with the known facts filled in",
		Long: `Print an evidence record for one row with everything camp can establish
already filled in, and the judgment fields left empty.

The split is deliberate. Anchors and signals are facts camp measured - the
row's stage, its age, its content hash, its workflow status. The empty fields
are what a person or an agent has to decide. Nothing here guesses at what was
delivered; a template that did would produce a record asserting a conclusion
nobody reached.

No pull-request anchor is ever pre-filled: camp cannot observe a PR without
asking a remote, and an anchor claiming a state nobody checked would make a
verdict look verified when it is not.

Fill it in and submit it:
  camp triage evidence template <id> > /tmp/record.json
  camp triage evidence set <id> --file /tmp/record.json`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only record scaffold built from data camp already holds",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEvidenceTemplate(cmd, args[0], runID)
		},
	}
	cmd.Flags().StringVar(&runID, "run", "", "Use a specific run id instead of the latest")
	return cmd
}

func runEvidenceTemplate(cmd *cobra.Command, stableID, runID string) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	store := triage.NewStore(root, nil)

	runID, err = resolveRunID(ctx, store, runID)
	if err != nil {
		return err
	}
	row, err := findRow(ctx, store, runID, stableID)
	if err != nil {
		return err
	}

	// A live lookup enriches the template with signals the frozen row does not
	// carry (age, workflow status). Its absence is not an error: a row can
	// outlive the thing it describes, and the template still works.
	var live *workitem.WorkItem
	resolver := paths.NewResolverFromConfig(root, cfg)
	if items, discErr := workitem.Discover(ctx, root, resolver); discErr == nil {
		for i := range items {
			if items[i].StableID == row.StableID || items[i].Key == row.Key {
				live = &items[i]
				break
			}
		}
	}

	record, err := triage.BuildEvidenceTemplate(ctx, triage.TemplateInput{
		CampaignRoot: root,
		Row:          *row,
		Item:         live,
		Now:          triage.SystemClock(),
	})
	if err != nil {
		return err
	}

	body, err := triage.MarshalTemplate(record)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(body)
	return err
}

// findRow resolves a stable id against the run's manifest, so an unknown id
// fails naming itself rather than writing a record for a row that is not in
// the run.
func findRow(ctx context.Context, store *triage.Store, runID, stableID string) (*triage.ManifestRow, error) {
	run, err := store.OpenRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	for i := range run.Manifest.Rows {
		if run.Manifest.Rows[i].StableID == stableID {
			return &run.Manifest.Rows[i], nil
		}
	}
	return nil, camperrors.NewNotFound("triage row", stableID+" in run "+runID, nil)
}

func init() {
	Cmd.AddCommand(newEvidenceCommand())
}
