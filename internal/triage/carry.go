package triage

import (
	"sort"
	"strings"
)

// CarryDecision is whether a row's carried verdict survives, and why not when
// it does not.
type CarryDecision struct {
	Carried bool
	// Reason explains a lost carry, and is empty when the carry stands. Spec
	// doc 04 requires every lost carry be explainable: a verdict that quietly
	// stopped counting is worse than one that was never carried, because the
	// operator has no way to notice.
	Reason string
}

// CarryInput is everything the carry rules read. All of it is snapshot data:
// deciding a carry involves no I/O.
type CarryInput struct {
	Row     ManifestRow
	Verdict RowVerdict
	// Class is the row's refresh classification.
	Class RowClass
	// Base is the resolved profile the carried verdict was formed under, as
	// embedded in the run that produced it.
	Base ResolvedProfile
	// Next is the resolved profile in force now.
	Next ResolvedProfile
	// BasePolicy is the type policy the carried verdict was formed under, and
	// NextPolicy the one in force now. Zero values fall back to the built-in,
	// which is what a run that predates frozen policies has.
	BasePolicy TypePolicy
	NextPolicy TypePolicy
}

// DecideCarry applies spec doc 04's carry-forward invalidation rules.
//
// A carried verdict (D4) survives only while everything it depended on is
// unchanged. The rules are checked in order of how badly they invalidate:
// whether there is a verdict at all, then whether the world moved under it,
// then whether the policy that produced it moved under it.
//
// Pure function: no I/O, no clock. The profile comparison is a comparison of
// the two embedded resolved profiles and nothing else, so a carry decision can
// be re-derived and audited long after the run.
func DecideCarry(in CarryInput) CarryDecision {
	if !HasLiveProposal(in.Verdict) {
		return CarryDecision{Reason: "no live verdict to carry"}
	}

	// The world moved. The diff already stated exactly how, so reuse its
	// wording rather than inventing a second vocabulary for the same fact.
	if !in.Class.Applicable() {
		return CarryDecision{Reason: "row classified " + string(in.Class) +
			"; a carried verdict does not survive its own evidence"}
	}

	deltas := ProfileDeltaTouching(in.Row, in.Base, in.Next)
	// The vocabularies the two runs actually froze, which is the comparison
	// that matters once type policies come off disk rather than a builtin.
	deltas = append(deltas, TypePolicyDelta(
		in.policyOrBuiltin(in.BasePolicy), in.policyOrBuiltin(in.NextPolicy))...)
	if len(deltas) > 0 {
		return CarryDecision{Reason: "the resolved profile changed in a key that touches this row: " +
			strings.Join(deltas, ", ")}
	}

	// The disposition has to still mean something. A vocabulary that dropped
	// the label the verdict was expressed in leaves a verdict camp cannot
	// execute, which is a lost carry rather than an error.
	if in.Verdict.Disposition != "" {
		policy := in.policyOrBuiltin(in.NextPolicy)
		if _, err := ResolveDisposition(policy, in.Verdict.Disposition); err != nil {
			return CarryDecision{Reason: "disposition " + quote(in.Verdict.Disposition) +
				" no longer maps to an action for type " + quote(in.Row.Type)}
		}
	}

	return CarryDecision{Carried: true}
}

// policyOrBuiltin falls back to the built-in for a run that froze no policies.
func (in CarryInput) policyOrBuiltin(policy TypePolicy) TypePolicy {
	if len(policy.Dispositions) == 0 {
		return TypePolicyFor(in.Row.Type)
	}
	return policy
}

// ProfileDeltaTouching reports the profile changes that invalidate one row's
// carried verdict, in stable order. An empty result means every difference
// between the two profiles is cosmetic for this row.
//
// Spec doc 04 names exactly three touching dimensions: the row's type policy,
// its lane's evidence depth, and the disposition vocabulary that produced its
// verdict. Everything else — batch size, export path, routing tiers,
// concurrency, grouping, staleness reminders — is cosmetic and never
// invalidates. That asymmetry is the point: an incremental run is only worth
// having if ordinary profile tuning does not throw away every verdict.
func ProfileDeltaTouching(row ManifestRow, base, next ResolvedProfile) []string {
	var deltas []string

	if before, after := evidenceDepthFor(row, base), evidenceDepthFor(row, next); before != after {
		deltas = append(deltas, "evidence depth for stage "+
			quote(stageKeyFor(row))+" "+quote(string(before))+" -> "+quote(string(after)))
	}

	deltas = append(deltas, TypePolicyDelta(
		typePolicyFrom(base, row.Type), typePolicyFrom(next, row.Type))...)

	sort.Strings(deltas)
	return deltas
}

// TypePolicyDelta reports how two policies for the same type differ in ways
// that touch a verdict: the vocabulary offered, what each label resolves to,
// and the evidence depth the type asks for.
//
// The routing tier is deliberately absent. It is advisory metadata camp never
// acts on (D5), so a campaign retuning which model reads a lane must not
// invalidate verdicts already formed.
func TypePolicyDelta(base, next TypePolicy) []string {
	var deltas []string

	if base.Evidence != next.Evidence {
		deltas = append(deltas, "type evidence depth "+
			quote(string(base.Evidence))+" -> "+quote(string(next.Evidence)))
	}

	for _, label := range unionOfLabels(base, next) {
		before, hadBefore := base.Dispositions[label]
		after, hasAfter := next.Dispositions[label]
		switch {
		case hadBefore && !hasAfter:
			deltas = append(deltas, "disposition "+quote(label)+" was removed")
		case !hadBefore && hasAfter:
			deltas = append(deltas, "disposition "+quote(label)+" was added")
		case before != after:
			deltas = append(deltas, "disposition "+quote(label)+" now performs "+
				quote(string(after))+" instead of "+quote(string(before)))
		}
	}
	return deltas
}

// typePolicyFrom resolves one type's policy out of a profile.
//
// The seam spec doc 05 describes: type policies ship as builtins today and
// move into the profile in the profile sequence. When they do, only this
// function changes, and the carry rules above keep working unmodified.
func typePolicyFrom(_ ResolvedProfile, wfType string) TypePolicy {
	return TypePolicyFor(wfType)
}

// evidenceDepthFor is the depth a profile assigns to the row's lane.
func evidenceDepthFor(row ManifestRow, profile ResolvedProfile) EvidenceDepth {
	depth, ok := profile.Evidence.DepthByStage[stageKeyFor(row)]
	if !ok {
		return EvidenceDepthMetadata
	}
	return depth
}

// stageKeyFor is the key a row's attention stage indexes the profile under,
// matching policyFor so the carry rules read the same table the snapshot did.
func stageKeyFor(row ManifestRow) string {
	if row.AttentionStage == "" {
		return EvidenceStageNone
	}
	return row.AttentionStage
}

// unionOfLabels lists every disposition either policy offers, sorted.
func unionOfLabels(base, next TypePolicy) []string {
	seen := make(map[string]bool, len(base.Dispositions)+len(next.Dispositions))
	for label := range base.Dispositions {
		seen[label] = true
	}
	for label := range next.Dispositions {
		seen[label] = true
	}
	labels := make([]string, 0, len(seen))
	for label := range seen {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// CarryLoss is one row whose carried verdict did not survive, with the reason.
type CarryLoss struct {
	StableID string `json:"stable_id"`
	Reason   string `json:"reason"`
}

// CarryForward decides which of a base run's verdicts survive into a new one.
//
// This is D4: the second triage of a campaign should be small, because most of
// it has not changed. A row carries when the base run actually approved a
// verdict for it, its identity resolves unchanged, its evidence anchors still
// observe what they observed, and the policy that produced the verdict still
// means the same thing.
//
// Pure: the caller supplies the base run's rows and verdicts, the new
// manifest's rows, and the classification the refresh machinery produced.
func CarryForward(in CarryForwardInput) CarryForwardResult {
	baseRows := indexRowsByID(in.BaseRows)
	classes := in.Classes

	result := CarryForwardResult{Losses: []CarryLoss{}}
	for i := range in.Rows {
		row := &in.Rows[i]
		verdict, judged := in.BaseVerdicts[row.StableID]
		if !judged || verdict.State == VerdictNone {
			continue
		}

		// A proposal is not a decision, and this is the only place that
		// distinction can be enforced: a carried row leaves the queue and the
		// review gate, and the new run holds no verdict for it, so carrying
		// something nobody approved would retire the human's pending approval
		// without anyone deciding to. Re-queuing costs one re-judgment; the
		// other way loses the decision and says nothing.
		//
		// The refresh path re-decides carried rows too, but asks a different
		// question — does a verdict that already carried still stand — so the
		// rule lives here rather than in DecideCarry.
		if verdict.State != VerdictApproved {
			result.Losses = append(result.Losses, CarryLoss{
				StableID: row.StableID,
				Reason:   carryRefusalFor(verdict),
			})
			continue
		}

		class, classified := classes[row.StableID]
		if !classified {
			// A row the diff never classified is new to this run.
			class = ClassNew
		}

		decision := DecideCarry(CarryInput{
			Row:        baseRowOr(baseRows, row.StableID, *row),
			Verdict:    verdict,
			Class:      class,
			Base:       in.BaseProfile,
			Next:       in.NextProfile,
			BasePolicy: in.BasePolicies[row.Type],
			NextPolicy: in.NextPolicies[row.Type],
		})
		if !decision.Carried {
			result.Losses = append(result.Losses, CarryLoss{
				StableID: row.StableID, Reason: decision.Reason,
			})
			continue
		}

		base := in.BaseRunID
		row.CarriedFrom = &base
		result.Carried = append(result.Carried, row.StableID)
	}
	return result
}

// carryRefusalFor names why a base-run verdict was not a decision to carry.
//
// A proposal and a refusal are different stories to an operator: one is work
// still owed a human, the other is a row that was looked at and turned down.
// Reporting both as "nothing to carry" would hide the first inside the second.
func carryRefusalFor(verdict RowVerdict) string {
	if verdict.State == VerdictProposed {
		return "the base run's proposal was never approved"
	}
	return "the base run's verdict was " + string(verdict.State) + ", not approved"
}

// CarryForwardInput is everything the carry decision reads.
type CarryForwardInput struct {
	BaseRunID string
	// Rows is the new manifest's rows, mutated in place to record a carry.
	Rows         []ManifestRow
	BaseRows     []ManifestRow
	BaseVerdicts map[string]RowVerdict
	// Classes is each row's staleness classification against the base run.
	Classes      map[string]RowClass
	BaseProfile  ResolvedProfile
	NextProfile  ResolvedProfile
	BasePolicies map[string]TypePolicy
	NextPolicies map[string]TypePolicy
}

// CarryForwardResult is what carried and what did not, with reasons.
type CarryForwardResult struct {
	Carried []string
	Losses  []CarryLoss
}

// indexRowsByID keys rows by stable id.
func indexRowsByID(rows []ManifestRow) map[string]ManifestRow {
	out := make(map[string]ManifestRow, len(rows))
	for _, row := range rows {
		out[row.StableID] = row
	}
	return out
}

// baseRowOr prefers the base run's frozen row, since that is what the verdict
// was formed against.
func baseRowOr(base map[string]ManifestRow, id string, fallback ManifestRow) ManifestRow {
	if row, ok := base[id]; ok {
		return row
	}
	return fallback
}
