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

	if deltas := ProfileDeltaTouching(in.Row, in.Base, in.Next); len(deltas) > 0 {
		return CarryDecision{Reason: "the resolved profile changed in a key that touches this row: " +
			strings.Join(deltas, ", ")}
	}

	// The disposition has to still mean something. A vocabulary that dropped
	// the label the verdict was expressed in leaves a verdict camp cannot
	// execute, which is a lost carry rather than an error.
	if in.Verdict.Disposition != "" {
		policy := TypePolicyFor(in.Row.Type)
		if _, err := ResolveDisposition(policy, in.Verdict.Disposition); err != nil {
			return CarryDecision{Reason: "disposition " + quote(in.Verdict.Disposition) +
				" no longer maps to an action for type " + quote(in.Row.Type)}
		}
	}

	return CarryDecision{Carried: true}
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
