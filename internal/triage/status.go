package triage

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// RowState is where one row stands in the run, derived from its manifest entry
// and its folded verdict.
type RowState string

const (
	// RowPendingEvidence means nothing has been submitted for the row yet.
	RowPendingEvidence RowState = "pending-evidence"
	// RowProposed means a proposal awaits approval.
	RowProposed RowState = "proposed"
	// RowApproved means the verdict is ready for apply.
	RowApproved RowState = "approved"
	// RowRejected means the proposal was refused and the row re-queues.
	RowRejected RowState = "rejected"
	// RowStale means refresh invalidated the verdict.
	RowStale RowState = "stale"
	// RowApplied means the verdict executed and a receipt exists.
	RowApplied RowState = "applied"
	// RowVerified means discovery confirmed the applied result.
	RowVerified RowState = "verified"
	// RowCarried means the verdict came forward from a base run.
	RowCarried RowState = "carried"
)

// RowStates returns the row-state vocabulary in lifecycle order.
func RowStates() []string {
	return []string{
		string(RowPendingEvidence),
		string(RowProposed),
		string(RowApproved),
		string(RowRejected),
		string(RowStale),
		string(RowApplied),
		string(RowVerified),
		string(RowCarried),
	}
}

// BatchProgress is one review batch's standing.
type BatchProgress struct {
	Batch    int `json:"batch"`
	Rows     int `json:"rows"`
	Decided  int `json:"decided"`
	Approved int `json:"approved"`
}

// Consolidation is one unfinished split from a `consolidate` verdict.
//
// The queue is always present in the payload and empty until the split verb
// exists, so a consumer written against this shape does not change when it
// starts filling.
type Consolidation struct {
	StableID   string   `json:"stable_id"`
	Successors []string `json:"successors"`
	Missing    []string `json:"missing"`
}

// Status is the whole answer `camp triage status` reports.
type Status struct {
	RunID   string  `json:"run_id"`
	Phase   Phase   `json:"phase"`
	Mode    RunMode `json:"mode"`
	Profile string  `json:"profile"`
	Active  bool    `json:"active"`
	// AbandonReason is set only on an abandoned run that recorded one.
	AbandonReason string `json:"abandon_reason,omitempty"`
	Rows          int    `json:"rows"`
	// Counts holds every state in RowStates(), including zeros, so a caller
	// can index it without checking for presence.
	Counts         map[string]int  `json:"counts"`
	Batches        []BatchProgress `json:"batches"`
	Consolidations []Consolidation `json:"consolidations"`
	// CarryLosses names every row that lost a carried verdict, with the
	// reason refresh recorded. Spec doc 04 requires status be able to answer
	// why a row was re-queued rather than carried.
	CarryLosses    []CarryLoss `json:"carry_losses"`
	IdentityIssues int         `json:"identity_exceptions"`
	CreatedAt      string      `json:"created_at"`
}

// BuildStatus derives a run's status from run data alone.
//
// No discovery walk happens here. Status answers "where is this session",
// which must be instant and must keep meaning even when the campaign has moved
// underneath the run. Comparing the session against the live campaign is what
// refresh is for, and conflating the two would make status quietly expensive
// and quietly wrong.
func BuildStatus(ctx context.Context, store *Store, runID string) (*Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run, err := store.OpenRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	verdicts, err := store.Verdicts(ctx, runID)
	if err != nil {
		return nil, err
	}
	status := StatusFrom(run, verdicts)

	// The consolidation queue needs the world, which is why it is filled here
	// rather than in StatusFrom: FT-014 asked for the unfinished splits as a
	// work queue, and "unfinished" is a question about what exists now.
	successors, err := store.SuccessorsByRow(ctx, runID, verdicts)
	if err != nil {
		return nil, err
	}
	if len(successors) > 0 {
		wanted := map[string]bool{}
		for _, ids := range successors {
			for _, id := range ids {
				wanted[id] = true
			}
		}
		discovered, err := store.discoveredIDsForStatus(ctx, wanted)
		if err != nil {
			return nil, err
		}
		status.Consolidations = ConsolidationQueue(ConsolidationInput{
			Rows: run.Manifest.Rows, Verdicts: verdicts,
			Successors: successors, Discovered: discovered,
		})
	}
	return status, nil
}

// discoveredIDsForStatus indexes what currently exists, including dungeoned
// items: a successor that was itself completed still counts as existing, the
// same rule the retirement gate applies.
func (s *Store) discoveredIDsForStatus(ctx context.Context, wanted map[string]bool) (map[string]bool, error) {
	cfg, err := config.LoadCampaignConfig(ctx, s.campaignRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "loading the campaign config")
	}
	items, err := workitem.Discover(ctx, s.campaignRoot, paths.NewResolverFromConfig(s.campaignRoot, cfg))
	if err != nil {
		return nil, camperrors.Wrap(err, "discovering work items")
	}
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[StableIDFor(item)] = true
	}

	// A successor that was itself completed still counts as existing — the
	// same rule the retirement gate applies, so status and the gate cannot
	// disagree about whether a parent is ready to retire.
	stillMissing := map[string]bool{}
	for id := range wanted {
		if !out[id] {
			stillMissing[id] = true
		}
	}
	dungeoned, err := workitem.FindDungeonedIDs(ctx, s.campaignRoot, stillMissing)
	if err != nil {
		return out, nil //nolint:nilerr // a dungeon scan failure must not fail status
	}
	for id := range dungeoned {
		out[id] = true
	}
	return out, nil
}

// StatusFrom folds a run and its verdicts into a status. Pure, so the shape
// can be tested without a filesystem.
func StatusFrom(run *Run, verdicts map[string]RowVerdict) *Status {
	status := &Status{
		RunID:          run.ID,
		Phase:          run.State.Phase,
		Mode:           run.Manifest.Mode,
		Profile:        run.Manifest.Profile.Name,
		Active:         run.Active(),
		Rows:           len(run.Manifest.Rows),
		Counts:         emptyCounts(),
		Batches:        []BatchProgress{},
		Consolidations: []Consolidation{},
		CarryLosses:    []CarryLoss{},
		CreatedAt:      run.Manifest.CreatedAt.Format(time.RFC3339),
	}
	if run.State.AbandonReason != nil {
		status.AbandonReason = *run.State.AbandonReason
	}

	// Losses frozen at start — rows that held a verdict in the base run and
	// did not carry it — plus the ones a refresh produced later. Both are the
	// same question to an operator: why is this row in front of me again.
	status.CarryLosses = append(status.CarryLosses, run.Manifest.CarryLosses...)

	byBatch := map[int]*BatchProgress{}
	for _, row := range run.Manifest.Rows {
		state := rowStateFor(row, verdicts[row.StableID])
		status.Counts[string(state)]++
		if row.IdentityException != nil {
			status.IdentityIssues++
		}
		if loss, ok := carryLossFor(row, verdicts[row.StableID]); ok {
			status.CarryLosses = append(status.CarryLosses, loss)
		}

		progress, ok := byBatch[row.Batch]
		if !ok {
			progress = &BatchProgress{Batch: row.Batch}
			byBatch[row.Batch] = progress
		}
		progress.Rows++
		switch state {
		case RowApproved, RowApplied, RowVerified, RowCarried:
			progress.Decided++
			progress.Approved++
		case RowRejected:
			progress.Decided++
		}
	}

	for _, batch := range sortedBatches(byBatch) {
		status.Batches = append(status.Batches, *batch)
	}
	// Stable order, so two reads of an unchanged run agree and a diff of two
	// status payloads shows only what actually moved.
	sort.Slice(status.CarryLosses, func(a, b int) bool {
		return status.CarryLosses[a].StableID < status.CarryLosses[b].StableID
	})
	return status
}

// carryLossFor reports why a carried row lost its verdict, reading the reason
// out of the stale event refresh appended.
//
// Nothing extra is stored to answer this: the decision stream already carries
// the reason as the note on the retiring event, and the fold surfaces it. A
// separate record of the same fact could disagree with the stream, and the
// stream is the one that decides what the row's verdict actually is.
func carryLossFor(row ManifestRow, verdict RowVerdict) (CarryLoss, bool) {
	if row.CarriedFrom == nil || verdict.State != VerdictStale || verdict.Note == "" {
		return CarryLoss{}, false
	}
	return CarryLoss{StableID: row.StableID, Reason: verdict.Note}, true
}

// rowStateFor decides a row's state from its manifest entry and verdict.
//
// A carried row is reported as carried even though it holds an approved
// verdict: the operator needs to know which decisions this run made and which
// it inherited, because those are the ones a changed anchor can invalidate.
func rowStateFor(row ManifestRow, verdict RowVerdict) RowState {
	if row.CarriedFrom != nil && verdict.State == VerdictNone {
		return RowCarried
	}
	switch verdict.State {
	case VerdictProposed:
		return RowProposed
	case VerdictApproved:
		return RowApproved
	case VerdictRejected:
		return RowRejected
	case VerdictStale:
		return RowStale
	case VerdictSuperseded:
		return RowPendingEvidence
	default:
		return RowPendingEvidence
	}
}

// emptyCounts seeds every state at zero so the JSON shape is fixed.
func emptyCounts() map[string]int {
	counts := make(map[string]int, len(RowStates()))
	for _, state := range RowStates() {
		counts[state] = 0
	}
	return counts
}

// sortedBatches returns batch progress in batch order.
func sortedBatches(byBatch map[int]*BatchProgress) []*BatchProgress {
	out := make([]*BatchProgress, 0, len(byBatch))
	for _, progress := range byBatch {
		out = append(out, progress)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Batch < out[b].Batch })
	return out
}

// recordedSuccessors reads the successors a parent's marker declares, or nil
// when it has none yet.
func (s *Store) recordedSuccessors(ctx context.Context, stableID string) []string {
	cfg, err := config.LoadCampaignConfig(ctx, s.campaignRoot)
	if err != nil {
		return nil
	}
	items, err := workitem.Discover(ctx, s.campaignRoot, paths.NewResolverFromConfig(s.campaignRoot, cfg))
	if err != nil {
		return nil
	}
	for _, item := range items {
		if StableIDFor(item) != stableID {
			continue
		}
		return splitIntoAt(ctx, filepath.Join(s.campaignRoot, item.RelativePath))
	}

	// The parent may have retired already — a consolidation ends with the
	// parent in the dungeon, and Discover skips dungeons. Its marker is still
	// the record of what it split into, so the queue keeps reporting the real
	// successors after the split rather than falling back to declared names
	// and disagreeing with the gate.
	if dir := findDungeonedDir(ctx, s.campaignRoot, stableID); dir != "" {
		return splitIntoAt(ctx, dir)
	}
	return nil
}

// splitIntoAt reads a marker's declared successors.
func splitIntoAt(ctx context.Context, dir string) []string {
	meta, err := workitem.LoadMetadata(ctx, dir)
	if err != nil || meta == nil {
		return nil
	}
	return workitem.SplitIntoOf(meta)
}

// findDungeonedDir locates a retired workitem's directory by stable id.
func findDungeonedDir(ctx context.Context, campaignRoot, stableID string) string {
	found, err := workitem.FindDungeonedPaths(ctx, campaignRoot, map[string]bool{stableID: true})
	if err != nil {
		return ""
	}
	return found[stableID]
}

// ConsolidationInput is what the consolidation queue reads. All snapshots.
type ConsolidationInput struct {
	Rows     []ManifestRow
	Verdicts map[string]RowVerdict
	// Successors names each row's declared successors, from its rationale.
	Successors map[string][]string
	// Discovered is the set of stable ids that currently exist.
	Discovered map[string]bool
}

// ConsolidationQueue derives the unfinished consolidations: every row whose
// verdict is a split, with its declared successors and which of them are
// missing.
//
// One derivation, three consumers — `camp triage status`, the review TUI's
// consolidate card, and verify's lineage check. A second implementation of
// "which successors are missing" would eventually disagree with this one, and
// the disagreement would show up as a parent that status says is ready and the
// gate refuses to retire.
//
// Pure: no I/O, no clock. The caller supplies the discovery set.
func ConsolidationQueue(in ConsolidationInput) []Consolidation {
	out := []Consolidation{}
	for _, row := range in.Rows {
		verdict := in.Verdicts[row.StableID]
		if verdict.CanonicalAction != ActionSplit {
			continue
		}
		// Explicitly non-nil: a nil slice marshals to null and breaks naive
		// consumers of the status contract.
		successors := []string{}
		successors = append(successors, in.Successors[row.StableID]...)
		sort.Strings(successors)

		missing := []string{}
		for _, successor := range successors {
			if !in.Discovered[successor] {
				missing = append(missing, successor)
			}
		}
		out = append(out, Consolidation{
			StableID:   row.StableID,
			Successors: successors,
			Missing:    missing,
		})
	}
	return out
}

// Blocked reports whether this consolidation's parent is still held by the
// retirement gate.
func (c Consolidation) Blocked() bool { return len(c.Missing) > 0 }

// SuccessorsByRow reads each row's declared successors from its rationale.
//
// The rationale is the argument for the verdict, so the successor list lives
// with it: "retire this once these exist" is one claim, not two.
func (s *Store) SuccessorsByRow(ctx context.Context, runID string, verdicts map[string]RowVerdict) (map[string][]string, error) {
	out := make(map[string][]string, len(verdicts))
	for _, id := range sortedKeys(verdicts) {
		verdict := verdicts[id]
		if verdict.CanonicalAction != ActionSplit || verdict.RationaleRef == "" {
			continue
		}
		rationale, err := s.Rationale(ctx, runID, id)
		if err != nil || rationale == nil {
			continue
		}
		if len(rationale.Successors) > 0 {
			out[id] = rationale.Successors
		}

		// Once the split has run, the parent's marker records the ids that
		// were actually created, which are generated and so never equal the
		// names the rationale declared. Preferring the marker is what keeps
		// this queue agreeing with the retirement gate, which reads exactly
		// that field. Before the split there is no marker to read and the
		// declared names are the best answer available.
		if actual := s.recordedSuccessors(ctx, id); len(actual) > 0 {
			out[id] = actual
		}
	}
	return out, nil
}

// TriageBannerText returns the one-line notice high-traffic commands print
// when a campaign's last triage has gone stale, or "" when it has not.
//
// Shared wording, on the SweepBannerText pattern, so `camp status` and the
// workitem banner cannot drift into saying it two different ways.
//
// It takes counts rather than doing any discovery of its own. The notice sits
// in the path of commands people run constantly, and a banner that cost a
// filesystem walk would be a tax on every one of them.
func TriageBannerText(daysSince, staleAfterDays, changedRows int) string {
	switch {
	case changedRows > 0:
		noun := "workitems"
		verb := "have"
		if changedRows == 1 {
			noun, verb = "workitem", "has"
		}
		return strconv.Itoa(changedRows) + " " + noun + " " + verb +
			" changed since the last triage — run: camp triage start"
	case staleAfterDays > 0 && daysSince > staleAfterDays:
		return "last triage was " + strconv.Itoa(daysSince) +
			" days ago — run: camp triage start"
	default:
		return ""
	}
}

// NoticeFileName caches what the last refresh saw, so the banner can be
// answered without a discovery walk.
const NoticeFileName = "notice.json"

// Notice is the cached verdict the banner reads.
type Notice struct {
	SchemaVersion string    `json:"schema_version"`
	RunID         string    `json:"run_id"`
	CheckedAt     time.Time `json:"checked_at"`
	// ChangedRows is how many rows the last refresh found moved, gone, or new.
	ChangedRows int `json:"changed_rows"`
}

// Normalize implements Document.
func (n *Notice) Normalize() {
	n.SchemaVersion = SchemaVersion
	normalizeTime(&n.CheckedAt)
}

func (n *Notice) kind() string    { return "triage notice" }
func (n *Notice) version() string { return n.SchemaVersion }

// Validate implements Document.
func (n *Notice) Validate() []Violation {
	out := checkRequired("run_id", n.RunID)
	return append(out, checkTimeSet("checked_at", n.CheckedAt)...)
}

// NoticePath is where the cached verdict lives.
func (s *Store) NoticePath() string { return filepath.Join(s.root, NoticeFileName) }

// WriteNotice caches what a refresh saw.
func (s *Store) WriteNotice(ctx context.Context, notice *Notice) error {
	notice.Normalize()
	body, err := MarshalDocument(notice)
	if err != nil {
		return err
	}
	return s.writeLocked(ctx, s.NoticePath(), body)
}

// ReadNotice returns the cached verdict, or nil when there is none.
//
// A missing or unreadable notice is not an error: the banner is an
// optimization on top of a working campaign, and failing `camp status`
// because a cache file was malformed would be a poor trade.
func (s *Store) ReadNotice() *Notice {
	body, err := os.ReadFile(s.NoticePath())
	if err != nil {
		return nil
	}
	var notice Notice
	if err := ParseDocument(body, &notice, Lenient); err != nil {
		return nil
	}
	return &notice
}

// BannerFor returns the notice line for a campaign, or "" when there is
// nothing to say.
//
// The cached verdict is read first, so a campaign that has never triaged —
// which is most of them — answers with one failed stat and no further work.
// Only a campaign that has something to be stale about pays for the threshold.
//
// The threshold is the campaign's own. It is resolved here rather than passed
// in because the caller has no way to know it, and a caller that guessed would
// make `runs.stale_after_days` a key the operator can set and camp ignores.
// That costs one small config read, not the discovery walk the hot path rules
// out. A profile camp cannot read costs the notice its accuracy, never the
// command: the built-in threshold stands in.
func BannerFor(ctx context.Context, campaignRoot string, now time.Time) string {
	store := NewStore(campaignRoot, nil)

	notice := store.ReadNotice()
	if notice == nil {
		return ""
	}

	staleAfterDays := DefaultProfile().Runs.StaleAfterDays
	if profile, err := ResolveProfile(ctx, campaignRoot); err == nil {
		staleAfterDays = profile.Runs.StaleAfterDays
	}

	days := int(now.Sub(notice.CheckedAt).Hours() / 24)
	return TriageBannerText(days, staleAfterDays, notice.ChangedRows)
}
