package triage

import (
	"sort"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// SnapshotInput is everything needed to freeze an inventory into a manifest.
type SnapshotInput struct {
	ProfileName string
	Profile     ResolvedProfile
	Mode        RunMode
	// BaseRunID names the run an incremental run diffs against.
	BaseRunID *string
	// Items are the in-scope workitems, as discovery returned them.
	Items []workitem.WorkItem
	// Now is the snapshot time, injected so a manifest is reproducible.
	Now time.Time
}

// BuildManifest freezes items into a run manifest: one row per item, policy
// resolved from the profile, and a batch assignment.
//
// The function is pure. Given the same items, profile, and clock it produces a
// byte-identical manifest, which is what makes a run replayable and lets the
// determinism test compare two starts directly.
func BuildManifest(in SnapshotInput) (*Manifest, error) {
	profile := in.Profile
	profile.Normalize()

	rows := make([]ManifestRow, 0, len(in.Items))
	for _, item := range in.Items {
		rows = append(rows, rowFor(item, profile, in.Now))
	}
	rows = assignBatches(in.Items, rows, profile)

	manifest := &Manifest{
		RunID: "", // assigned by the store from its clock
		Mode:  in.Mode,
		Profile: ManifestProfile{
			Name:     in.ProfileName,
			Resolved: profile,
		},
		BaseRunID: in.BaseRunID,
		CreatedAt: in.Now,
		Rows:      rows,
	}
	manifest.Normalize()

	// Validate what the snapshot owns, so a bad row is reported here with its
	// index rather than surfacing later as an opaque store write failure. The
	// run id is not checked: the store assigns it, and the store validates the
	// complete document before writing it.
	if err := newValidationError("run manifest", manifest.validateContent()); err != nil {
		return nil, err
	}
	return manifest, nil
}

// rowFor maps one discovered workitem to its manifest row.
func rowFor(item workitem.WorkItem, profile ResolvedProfile, now time.Time) ManifestRow {
	ref, _ := item.SourceMetadata["ref"].(string)

	row := ManifestRow{
		StableID:       item.StableID,
		Ref:            ref,
		Key:            item.Key,
		Type:           string(item.WorkflowType),
		Title:          item.Title,
		RelativePath:   item.RelativePath,
		LifecycleStage: string(item.LifecycleStage),
		AttentionStage: item.AttentionStage,
		Policy:         policyFor(item, profile),
	}
	if row.LifecycleStage == "" {
		row.LifecycleStage = string(workitem.LifecycleStageNone)
	}

	// Identity, in descending order of durability.
	//
	// Only builtin doc types (design, explore) are supposed to carry a
	// .workitem marker, so only those can be missing one. Intents and
	// festivals never have a marker and are not defective for it: their
	// identity is the source id discovery already resolved, and flagging them
	// would put an exception on most of a real campaign and make the field
	// mean nothing. workitem.NeedsAdoption is camp's own predicate for the
	// FT-008 population, so triage and `camp workitem adopt` agree on who
	// needs repair.
	switch {
	case row.StableID != "":
	case item.SourceID != "":
		row.StableID = item.SourceID
	default:
		row.StableID = item.Key
	}

	if workitem.NeedsAdoption(&item) {
		row.IdentityException = &IdentityException{
			Reason:    "no .workitem marker: identity is bound to the path until adopted",
			Path:      item.RelativePath,
			GrantedBy: identityExceptionGrantor,
			GrantedAt: now,
		}
	}
	return row
}

// identityExceptionGrantor names camp itself as the grantor for exceptions the
// preflight recorded automatically, as opposed to one an operator granted.
const identityExceptionGrantor = "camp-triage-preflight"

// policyFor resolves the profile slice that applies to one row: how much
// evidence its lane asks for, and which model class a driver should read it
// with.
func policyFor(item workitem.WorkItem, profile ResolvedProfile) RowPolicy {
	stage := item.AttentionStage
	if stage == "" {
		stage = EvidenceStageNone
	}
	depth, ok := profile.Evidence.DepthByStage[stage]
	if !ok {
		depth = EvidenceDepthMetadata
	}
	return RowPolicy{
		Evidence:    depth,
		RoutingTier: profile.Routing.EvidenceTier,
	}
}

// assignBatches partitions rows into review batches and returns them in
// canonical order.
//
// Rows sort by (type, key) so neither the partition nor the file's contents
// depend on the order discovery happened to walk the filesystem in. Groups are
// then visited in sorted order and chunked by batch_size, numbering batches
// from 1 across the whole run. Two runs over an unchanged campaign therefore
// produce a byte-identical manifest, which is what lets an incremental run
// compare rows across runs at all.
//
// The returned slice is the emitted order, so batch numbers also read
// monotonically down the file.
func assignBatches(items []workitem.WorkItem, rows []ManifestRow, profile ResolvedProfile) []ManifestRow {
	// rows[i] was built from items[i], so one index addresses both.
	order := make([]int, len(rows))
	for i := range rows {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ra, rb := rows[order[a]], rows[order[b]]
		if ra.Type != rb.Type {
			return ra.Type < rb.Type
		}
		return ra.Key < rb.Key
	})

	grouped := make(map[string][]int, len(order))
	var groupOrder []string
	for _, i := range order {
		key := groupKeyFor(items[i], rows[i], profile.Review.GroupBy)
		if _, seen := grouped[key]; !seen {
			groupOrder = append(groupOrder, key)
		}
		grouped[key] = append(grouped[key], i)
	}
	sort.Strings(groupOrder)

	size := profile.Review.BatchSize
	if size < 1 {
		size = 1
	}
	batch := 0
	emitted := make([]ManifestRow, 0, len(rows))
	for _, key := range groupOrder {
		members := grouped[key]
		for i, rowIndex := range members {
			if i%size == 0 {
				batch++
			}
			row := rows[rowIndex]
			row.Batch = batch
			emitted = append(emitted, row)
		}
	}
	return emitted
}

// groupKeyFor is the review lane a row belongs to.
//
// A row lands in exactly one batch, so for the multi-valued dimensions (tag,
// project) the lane is the first value in sorted order rather than every value
// the item carries. Deterministic, and the alternative — a row appearing in
// several batches — would mean approving it more than once.
func groupKeyFor(item workitem.WorkItem, row ManifestRow, groupBy ReviewGroupBy) string {
	switch groupBy {
	case GroupByAttentionStage:
		if row.AttentionStage == "" {
			return EvidenceStageNone
		}
		return row.AttentionStage
	case GroupByProject:
		return firstOr(sortedCopy(item.Projects), "none")
	case GroupByTag:
		return firstOr(sortedCopy(item.Tags), "none")
	case GroupByType:
		return row.Type
	default:
		return row.Type
	}
}

// sortedCopy returns values sorted, leaving the input untouched. Used where a
// row must pick one value out of an unordered set deterministically.
func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// firstOr returns the first value, or fallback when there is none.
func firstOr(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return values[0]
}

// BatchCount reports how many batches a manifest partitions into.
func BatchCount(manifest *Manifest) int {
	highest := 0
	for _, row := range manifest.Rows {
		if row.Batch > highest {
			highest = row.Batch
		}
	}
	return highest
}

// IdentityExceptionCount reports how many rows triage without a durable id.
func IdentityExceptionCount(manifest *Manifest) int {
	count := 0
	for _, row := range manifest.Rows {
		if row.IdentityException != nil {
			count++
		}
	}
	return count
}

// ErrNoRowsInScope is returned when a scope selects nothing. Starting a run
// over zero rows is a scope mistake, not an empty campaign: the operator asked
// a question that has no subject.
var ErrNoRowsInScope = camperrors.New("no workitems in scope")
