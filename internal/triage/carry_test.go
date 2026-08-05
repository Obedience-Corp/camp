package triage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// carriedRow is a design row with a live carried verdict.
func carriedRow() ManifestRow {
	row := diffRow()
	row.Type = "design"
	row.AttentionStage = "active"
	row.CarriedFrom = ptr("run-20260803T234110Z")
	return row
}

func carriedVerdict(disposition string) RowVerdict {
	return RowVerdict{
		StableID:        carriedRow().StableID,
		State:           VerdictApproved,
		Disposition:     disposition,
		CanonicalAction: CanonicalAction("attention/parked"),
		Events:          2,
	}
}

// TestDecideCarry covers every invalidation rule and every cosmetic non-rule.
func TestDecideCarry(t *testing.T) {
	tests := []struct {
		name        string
		verdict     RowVerdict
		class       RowClass
		next        func(*ResolvedProfile)
		wantCarried bool
		wantReason  []string
	}{
		{
			name:        "a fresh row under an unchanged profile carries",
			verdict:     carriedVerdict("parked"),
			class:       ClassFresh,
			wantCarried: true,
		},
		{
			name:        "a moved row still carries; identity survives moves",
			verdict:     carriedVerdict("parked"),
			class:       ClassMoved,
			wantCarried: true,
		},
		{
			name:       "a changed row loses its carry",
			verdict:    carriedVerdict("parked"),
			class:      ClassChanged,
			wantReason: []string{"classified changed", "does not survive its own evidence"},
		},
		{
			name:       "a gone row loses its carry",
			verdict:    carriedVerdict("parked"),
			class:      ClassGone,
			wantReason: []string{"classified gone"},
		},
		{
			name:       "a row with no live verdict has nothing to carry",
			verdict:    RowVerdict{State: VerdictRejected, Disposition: "parked"},
			class:      ClassFresh,
			wantReason: []string{"no live verdict to carry"},
		},
		{
			name:       "a row never judged has nothing to carry",
			verdict:    RowVerdict{},
			class:      ClassFresh,
			wantReason: []string{"no live verdict to carry"},
		},
		{
			name:    "the row's lane changing evidence depth invalidates",
			verdict: carriedVerdict("parked"),
			class:   ClassFresh,
			next: func(p *ResolvedProfile) {
				p.Evidence.DepthByStage["active"] = EvidenceDepthNone
			},
			wantReason: []string{"touches this row", "evidence depth for stage", `"active"`},
		},
		{
			name:    "a different lane changing depth does not invalidate",
			verdict: carriedVerdict("parked"),
			class:   ClassFresh,
			next: func(p *ResolvedProfile) {
				p.Evidence.DepthByStage["parked"] = EvidenceDepthNone
			},
			wantCarried: true,
		},
		{
			name:    "batch size is cosmetic",
			verdict: carriedVerdict("parked"),
			class:   ClassFresh,
			next: func(p *ResolvedProfile) {
				p.Review.BatchSize = 50
			},
			wantCarried: true,
		},
		{
			name:    "the priorities export path is cosmetic",
			verdict: carriedVerdict("parked"),
			class:   ClassFresh,
			next: func(p *ResolvedProfile) {
				p.Outputs.PrioritiesExport = "docs/PRIORITIES.md"
			},
			wantCarried: true,
		},
		{
			name:    "routing tiers are cosmetic",
			verdict: carriedVerdict("parked"),
			class:   ClassFresh,
			next: func(p *ResolvedProfile) {
				p.Routing.EvidenceTier = RoutingTierStrong
				p.Routing.EscalationTier = RoutingTierCheap
				p.Routing.MaxConcurrent = 32
			},
			wantCarried: true,
		},
		{
			name:    "grouping and approval granularity are cosmetic",
			verdict: carriedVerdict("parked"),
			class:   ClassFresh,
			next: func(p *ResolvedProfile) {
				p.Review.GroupBy = GroupByProject
				p.Review.Approval = ApprovalRow
			},
			wantCarried: true,
		},
		{
			name:    "the staleness reminder is cosmetic",
			verdict: carriedVerdict("parked"),
			class:   ClassFresh,
			next: func(p *ResolvedProfile) {
				p.Runs.StaleAfterDays = 90
			},
			wantCarried: true,
		},
		{
			name:    "the anchor throttle is cosmetic",
			verdict: carriedVerdict("parked"),
			class:   ClassFresh,
			next: func(p *ResolvedProfile) {
				p.Anchors.RecheckMinutes = 120
			},
			wantCarried: true,
		},
		{
			name:       "a disposition that no longer maps invalidates",
			verdict:    carriedVerdict("not-a-real-disposition"),
			class:      ClassFresh,
			wantReason: []string{"no longer maps to an action", "not-a-real-disposition", "design"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := DefaultProfile()
			base.Normalize()
			next := DefaultProfile()
			next.Normalize()
			if tt.next != nil {
				tt.next(&next)
			}

			got := DecideCarry(CarryInput{
				Row: carriedRow(), Verdict: tt.verdict, Class: tt.class,
				Base: base, Next: next,
			})

			assert.Equal(t, tt.wantCarried, got.Carried)
			if tt.wantCarried {
				assert.Empty(t, got.Reason, "a surviving carry needs no explanation")
				return
			}
			for _, want := range tt.wantReason {
				assert.Contains(t, got.Reason, want)
			}
		})
	}
}

// TestProfileDeltaTouchingIgnoresCosmeticChange pins the asymmetry directly:
// an incremental run is only worth having if ordinary profile tuning does not
// throw away every verdict.
func TestProfileDeltaTouchingIgnoresCosmeticChange(t *testing.T) {
	base := DefaultProfile()
	base.Normalize()

	next := DefaultProfile()
	next.Normalize()
	next.Review.BatchSize = 99
	next.Review.GroupBy = GroupByTag
	next.Review.Approval = ApprovalRow
	next.Routing = ProfileRouting{
		EvidenceTier: RoutingTierStrong, EscalationTier: RoutingTierCheap,
		SynthesisTier: RoutingTierCheap, MaxConcurrent: 64,
	}
	next.Outputs = ProfileOutputs{PrioritiesExport: "docs/P.md", ScaffoldWorkflowDoc: true}
	next.Runs.StaleAfterDays = 365
	next.Anchors.RecheckMinutes = 240
	next.Scope.IncludeParked = false
	next.Preflight.Identity = IdentityPolicyStrict

	assert.Empty(t, ProfileDeltaTouching(carriedRow(), base, next),
		"every one of these is cosmetic for the row; none may invalidate a verdict")
}

// TestTypePolicyDelta covers the vocabulary comparison a carried verdict
// depends on. Type policies ship as builtins today, so this exercises the
// comparison directly rather than through a profile that cannot yet express
// the difference.
func TestTypePolicyDelta(t *testing.T) {
	base := TypePolicy{
		SchemaVersion: TypePolicySchemaVersion,
		Evidence:      EvidenceDepthDeep,
		RoutingTier:   RoutingTierDefault,
		Dispositions: map[string]CanonicalAction{
			"parked":    CanonicalAction("attention/parked"),
			"completed": CanonicalAction("dungeon/completed"),
		},
	}

	tests := []struct {
		name       string
		next       func(*TypePolicy)
		wantDeltas []string
	}{
		{
			name: "an identical policy has no delta",
			next: func(*TypePolicy) {},
		},
		{
			name: "the routing tier is advisory and never a delta",
			next: func(p *TypePolicy) { p.RoutingTier = RoutingTierStrong },
		},
		{
			name:       "a removed disposition is a delta",
			next:       func(p *TypePolicy) { delete(p.Dispositions, "completed") },
			wantDeltas: []string{`disposition "completed" was removed`},
		},
		{
			name: "an added disposition is a delta",
			next: func(p *TypePolicy) {
				p.Dispositions["consolidated"] = ActionSplit
			},
			wantDeltas: []string{`disposition "consolidated" was added`},
		},
		{
			name: "a re-pointed disposition is a delta",
			next: func(p *TypePolicy) {
				p.Dispositions["completed"] = CanonicalAction("dungeon/archived")
			},
			wantDeltas: []string{`disposition "completed" now performs`},
		},
		{
			name:       "the type's evidence depth is a delta",
			next:       func(p *TypePolicy) { p.Evidence = EvidenceDepthNone },
			wantDeltas: []string{"type evidence depth"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := TypePolicy{
				SchemaVersion: base.SchemaVersion,
				Evidence:      base.Evidence,
				RoutingTier:   base.RoutingTier,
				Dispositions:  map[string]CanonicalAction{},
			}
			for k, v := range base.Dispositions {
				next.Dispositions[k] = v
			}
			tt.next(&next)

			deltas := TypePolicyDelta(base, next)
			if len(tt.wantDeltas) == 0 {
				assert.Empty(t, deltas)
				return
			}
			require.Len(t, deltas, len(tt.wantDeltas))
			for i, want := range tt.wantDeltas {
				assert.Contains(t, deltas[i], want)
			}
		})
	}
}

// TestDecideCarryIsPure guards the reuse rule: the decision reads snapshots
// and must not mutate them.
func TestDecideCarryIsPure(t *testing.T) {
	row := carriedRow()
	base := DefaultProfile()
	base.Normalize()
	next := DefaultProfile()
	next.Normalize()

	beforeRow := row
	beforeBase := base

	DecideCarry(CarryInput{
		Row: row, Verdict: carriedVerdict("parked"), Class: ClassFresh,
		Base: base, Next: next,
	})

	assert.Equal(t, beforeRow, row)
	assert.Equal(t, beforeBase.Review, base.Review)
	assert.Equal(t, beforeBase.Evidence.DepthByStage, base.Evidence.DepthByStage)
}
