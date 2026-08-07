package triage

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// Expectation is what one applied receipt says the campaign should now look
// like. Derived purely from the receipt, never from the plan: verification
// checks what happened, not what was intended, which is the whole point of
// having receipts at all.
type Expectation struct {
	StableID string
	Kind     CommandKind
	// Path is the campaign-relative location the item should now be at.
	Path string
	// Stage is the attention stage it should now carry, empty when the
	// action did not set one.
	Stage string
	// Gone reports that the item should no longer be discoverable outside
	// dungeons, which is what a terminal promotion means.
	Gone bool
	// Successors must all exist for a split to have succeeded.
	Successors []string
}

// ExpectationFor derives what a receipt promises, given the manifest row it
// acted on.
//
// The moved-to path comes from the receipt's undo command, because that is
// where the executor recorded the destination the workitem actually landed at.
// Re-deriving it here would mean guessing the dated dungeon bucket a second
// time, and a verification that guesses is not proof of anything.
func ExpectationFor(receipt Receipt, row ManifestRow) Expectation {
	expectation := Expectation{
		StableID: receipt.StableID,
		Kind:     receipt.Kind,
		Path:     row.RelativePath,
	}

	switch receipt.Kind {
	case CommandKindAttention:
		expectation.Stage = argAfterStage(receipt.Argv)
		if expectation.Stage == stageClearArg {
			expectation.Stage = ""
		}
	case CommandKindDungeon:
		expectation.Gone = true
		if landed := undoSourcePath(receipt.Undo); landed != "" {
			expectation.Path = landed
		}
	case CommandKindRail:
		if landed := undoSourcePath(receipt.Undo); landed != "" {
			expectation.Path = landed
		}
	case CommandKindSplit:
		expectation.Successors = argsAfterFlags(receipt.Argv, "--into")
	case CommandKindIdea:
		// A retired idea leaves the discoverable set, exactly as a dungeoned
		// directory workitem does, so absence is the proof.
		//
		// A promoted one (ready, active) stays discoverable at a new path
		// under `.campaign/intents/<status>/`, and that path is not knowable
		// from the receipt: the service preserves a possibly-renamed basename,
		// so rebuilding it here would be a guess that looks authoritative.
		// Discoverability is what is checked instead, and the run's own
		// decision record carries the status.
		if ideaStatusIsDungeon(argAfterStage(receipt.Argv)) {
			expectation.Gone = true
		}
	}
	return expectation
}

// ideaStatusIsDungeon reports whether an idea status retires the idea out of
// the discoverable set.
func ideaStatusIsDungeon(status string) bool {
	switch status {
	case "done", "killed", "archived", "someday":
		return true
	}
	return false
}

// undoSourcePath reads the destination out of a `camp move <landed> <original>`
// undo, which is where the executor recorded what actually happened.
func undoSourcePath(undo string) string {
	fields := strings.Fields(undo)
	if len(fields) >= 4 && fields[0] == "camp" && fields[1] == "move" {
		return fields[2]
	}
	return ""
}

// VerifyInput is one verification pass.
type VerifyInput struct {
	RunID string
	// Items is a fresh discovery walk over the whole campaign.
	Items []workitem.WorkItem
	// Explanations accounts for a mismatch a human has already looked at,
	// keyed by stable id. A mismatch with an explanation does not fail the
	// verification; one without it does.
	Explanations map[string]string
	Now          time.Time
}

// Verify checks every applied receipt against a fresh discovery pass.
//
// Failing rows are reported, not hidden: an unexplained mismatch is the signal
// that the campaign is not in the state the approved decisions said it would
// be, and that is exactly the thing this command exists to surface.
func (s *Store) Verify(ctx context.Context, in VerifyInput) (*VerificationReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in.RunID == "" {
		return nil, camperrors.NewValidation("run_id", "is required", nil)
	}

	run, err := s.OpenRun(ctx, in.RunID)
	if err != nil {
		return nil, err
	}
	receipts, err := s.Receipts(ctx, in.RunID)
	if err != nil {
		return nil, err
	}

	index := IndexDiscovery(in.Items)
	rows := indexManifestRows(run.Manifest.Rows)

	applied := map[string]bool{}
	report := &VerificationReport{RunID: in.RunID, CheckedAt: in.Now}
	for _, receipt := range latestAppliedPerRow(receipts) {
		row := rows[receipt.StableID]
		expectation := ExpectationFor(receipt, row)
		applied[receipt.StableID] = true
		report.Rows = append(report.Rows,
			verifyOne(expectation, index, in.Explanations[receipt.StableID]))
	}

	// Every approved verdict that produced no successful receipt. Verifying
	// only the rows that applied made the report clean by omission: a halted
	// apply left its unexecuted decisions out of the tally entirely, and the
	// run went on to close as verified.
	verdicts, err := s.Verdicts(ctx, in.RunID)
	if err != nil {
		return nil, err
	}
	for _, row := range run.Manifest.Rows {
		if applied[row.StableID] || !verdicts[row.StableID].Applicable() {
			continue
		}
		report.Rows = append(report.Rows, VerificationRow{
			StableID:     row.StableID,
			ExpectedPath: row.RelativePath,
			Result:       VerificationUnapplied,
		})
	}
	sort.Slice(report.Rows, func(a, b int) bool {
		return report.Rows[a].StableID < report.Rows[b].StableID
	})

	report.Normalize()
	if err := newValidationError("verification report", report.Validate()); err != nil {
		return nil, err
	}
	return report, nil
}

// verifyOne compares one expectation against what discovery found.
func verifyOne(expectation Expectation, index DiscoveryIndex, explanation string) VerificationRow {
	row := VerificationRow{
		StableID:      expectation.StableID,
		ExpectedPath:  expectation.Path,
		ExpectedStage: expectation.Stage,
		Result:        VerificationMatch,
	}

	found, discoverable := index.ByStableID[expectation.StableID]
	if discoverable {
		row.DiscoveredPath = found.RelativePath
		row.DiscoveredStage = found.AttentionStage
	}

	switch {
	case expectation.Gone:
		// A terminal promotion means the item is no longer discoverable
		// outside dungeons. Still finding it is the mismatch.
		if discoverable {
			row.Result = VerificationMismatch
		} else {
			row.DiscoveredPath = expectation.Path
		}
	case !discoverable:
		// Everything else expects the item to still be there.
		row.Result = VerificationMismatch
	case expectation.Kind == CommandKindAttention:
		if found.AttentionStage != expectation.Stage {
			row.Result = VerificationMismatch
		}
	case expectation.Kind == CommandKindRail:
		if found.RelativePath != expectation.Path {
			row.Result = VerificationMismatch
		}
	case expectation.Kind == CommandKindSplit:
		if missing := missingSuccessors(expectation.Successors, index); len(missing) > 0 {
			row.Result = VerificationMismatch
			row.DiscoveredPath = "missing successors: " + strings.Join(missing, ", ")
		}
	}

	if row.Result == VerificationMismatch && explanation != "" {
		row.Explanation = explanation
	}
	return row
}

// missingSuccessors names the declared successors discovery cannot find.
func missingSuccessors(successors []string, index DiscoveryIndex) []string {
	var missing []string
	for _, successor := range successors {
		if _, ok := index.ByStableID[successor]; !ok {
			missing = append(missing, successor)
		}
	}
	return missing
}

// latestAppliedPerRow returns the last applied receipt for each row, in a
// stable order.
//
// A row can carry several receipts across passes — a failure then a retry —
// and only the last applied one describes the state the campaign is actually
// in. Verifying against an earlier one would report a mismatch that the retry
// already fixed.
func latestAppliedPerRow(receipts []Receipt) []Receipt {
	latest := make(map[string]Receipt, len(receipts))
	for _, receipt := range receipts {
		if receipt.Result == ReceiptApplied {
			latest[receipt.StableID] = receipt
		}
	}
	out := make([]Receipt, 0, len(latest))
	for _, id := range sortedKeys(latest) {
		out = append(out, latest[id])
	}
	return out
}

// indexManifestRows keys a manifest by stable id.
func indexManifestRows(rows []ManifestRow) map[string]ManifestRow {
	out := make(map[string]ManifestRow, len(rows))
	for _, row := range rows {
		out[row.StableID] = row
	}
	return out
}

// Unexplained returns the mismatching rows nobody has accounted for. These are
// what make `camp triage verify` exit non-zero.
func (v *VerificationReport) Unexplained() []VerificationRow {
	var out []VerificationRow
	for _, row := range v.Rows {
		// An unapplied row is unexplained by construction: the decision did
		// not happen, and no explanation of the campaign's state can account
		// for that. It keeps the run out of `verified`, which is the point.
		if row.Result == VerificationUnapplied {
			out = append(out, row)
			continue
		}
		if row.Result == VerificationMismatch && row.Explanation == "" {
			out = append(out, row)
		}
	}
	return out
}

// Clean reports whether every checked row matched or was explained.
func (v *VerificationReport) Clean() bool { return len(v.Unexplained()) == 0 }

// VerificationDocName is the rendered report beside verification.json.
const VerificationDocName = "VERIFICATION.md"

// WriteVerification records the report and its rendered document, and moves a
// clean run to `verified`.
//
// The phase moves only when the verification is clean. A run with unexplained
// mismatches is not verified in any sense worth recording, and marking it so
// would make the phase mean "someone ran verify" rather than "the campaign
// matches the decisions".
func (s *Store) WriteVerification(ctx context.Context, report *VerificationReport) (string, string, error) {
	dir := s.RunDir(report.RunID)

	body, err := MarshalDocument(report)
	if err != nil {
		return "", "", err
	}
	dataPath := filepath.Join(dir, VerificationFileName)
	if err := s.writeLocked(ctx, dataPath, body); err != nil {
		return "", "", err
	}

	docPath := filepath.Join(dir, VerificationDocName)
	if err := s.writeLocked(ctx, docPath, RenderVerification(report)); err != nil {
		return "", "", err
	}

	if report.Clean() {
		if err := s.AdvancePhase(ctx, report.RunID, PhaseVerified, "verification clean"); err != nil {
			return "", "", err
		}
	}
	return dataPath, docPath, nil
}

// RenderVerification renders VERIFICATION.md from the report.
//
// Pure and byte-stable for the same reason the review documents are: the
// report is the source of truth and the document is a view of it, so
// re-rendering an unchanged report is a no-op diff.
func RenderVerification(report *VerificationReport) []byte {
	var b strings.Builder

	b.WriteString("# Triage verification — " + report.RunID + "\n\n")
	b.WriteString("> Generated from run `" + report.RunID + "` by `camp triage verify`.\n" +
		"> **Do not edit.** Re-running verify replaces this file.\n")
	b.WriteString("\n**Run:** `" + report.RunID + "`  \n")
	b.WriteString("**Checked:** " + report.CheckedAt.UTC().Format(time.RFC3339) + "\n\n")

	b.WriteString("## Result\n\n")
	if report.Clean() {
		b.WriteString("Every applied row is where its approved verdict said it would be.\n\n")
	} else {
		b.WriteString("**" + strconv.Itoa(len(report.Unexplained())) +
			" row(s) are not where their verdict said they would be.**\n\n")
		b.WriteString("Each is listed below. Add an explanation if the difference is " +
			"accounted for, or move the workitem back and re-apply.\n\n")
	}

	b.WriteString("| Checked | Matched | Mismatched |\n| --- | --- | --- |\n")
	b.WriteString("| " + strconv.Itoa(report.Totals.Checked) + " | " + strconv.Itoa(report.Totals.Matched) +
		" | " + strconv.Itoa(report.Totals.Mismatched) + " |\n\n")

	if len(report.Rows) == 0 {
		b.WriteString("No applied rows to check. Run `camp triage apply` first.\n")
		return []byte(b.String())
	}

	b.WriteString("## Rows\n\n")
	b.WriteString("| Workitem | Result | Expected | Discovered | Explanation |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, row := range report.Rows {
		b.WriteString("| `" + row.StableID + "` | " + string(row.Result) + " | " +
			verificationCell(row.ExpectedPath, row.ExpectedStage) + " | " +
			verificationCell(row.DiscoveredPath, row.DiscoveredStage) + " | " +
			orDash(row.Explanation) + " |\n")
	}
	return []byte(b.String())
}

// verificationCell renders a location and stage into one cell.
func verificationCell(path, stage string) string {
	switch {
	case path != "" && stage != "":
		return "`" + path + "` (" + stage + ")"
	case path != "":
		return "`" + path + "`"
	case stage != "":
		return stage
	default:
		return "—"
	}
}

// orDash renders empty text as a dash so a table cell is never blank.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
