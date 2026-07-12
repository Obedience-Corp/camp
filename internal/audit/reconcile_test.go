package audit

import (
	"testing"

	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageMatchingAcrossIdSchemes(t *testing.T) {
	// The ledger already captures intent i1 (a live ULID event) and a festival
	// transition (a backfill bf_ event). Reconciliation must recognize both as
	// covered even though neither id matches a derived rc_ id - coverage is by
	// content (scope + kind), not id.
	events := []*ledgerkit.Event{
		{ID: "01ARZ...ulid", Kind: ledgerkit.KindCreated, Scope: ledgerkit.Scope{Intent: "i1"}},
		{ID: "bf_deadbeef", Kind: ledgerkit.KindTransitioned, Scope: ledgerkit.Scope{Festival: "CA0002"},
			Payload: map[string]any{"from": "ready", "to": "active"}},
	}
	captured := capturedIndex(events)

	coveredIntent := DerivedFact{Kind: ledgerkit.KindCreated, Scope: ledgerkit.Scope{Intent: "i1"}, IdentityKey: "intent-created:i1"}
	assert.True(t, captured[factCoverageKey(coveredIntent)], "intent already captured live is covered")

	coveredTransition := DerivedFact{Kind: ledgerkit.KindTransitioned, Scope: ledgerkit.Scope{Festival: "CA0002"},
		Payload:     map[string]any{"from": "ready", "to": "active"},
		IdentityKey: "fest-transitioned:CA0002:ready:active:0"}
	assert.True(t, captured[factCoverageKey(coveredTransition)], "festival transition already backfilled is covered")

	// A second ready→active occurrence is a gap under occurrence-faithful coverage.
	secondBounce := DerivedFact{Kind: ledgerkit.KindTransitioned, Scope: ledgerkit.Scope{Festival: "CA0002"},
		Payload:     map[string]any{"from": "ready", "to": "active"},
		IdentityKey: "fest-transitioned:CA0002:ready:active:1"}
	assert.False(t, captured[factCoverageKey(secondBounce)], "second bounce of the same edge remains a gap")

	// A gap: an intent with no event in the ledger.
	gap := DerivedFact{Kind: ledgerkit.KindCreated, Scope: ledgerkit.Scope{Intent: "i2"}, IdentityKey: "intent-created:i2"}
	assert.False(t, captured[factCoverageKey(gap)], "an unrepresented intent is a reconciliation gap")
}

func TestReconciledIdIsDeterministicAndPrefixed(t *testing.T) {
	// The same gap always derives the same rc_ id, so re-running reconciliation
	// converges (idempotence): once emitted, the event is in the ledger and
	// covered on the next pass.
	a := ledgerkit.DerivedID("rc", "intent-created:i2")
	b := ledgerkit.DerivedID("rc", "intent-created:i2")
	require.Equal(t, a, b)
	assert.Contains(t, a, "rc_")
}

func TestCanonicalFestivalID(t *testing.T) {
	assert.Equal(t, "CA0002", canonicalFestivalID("campaign-audit-trail-CA0002"))
	assert.Equal(t, "RI0002", canonicalFestivalID("remaining-intent-followups-RI0002"))
	// No trailing id token: fall back to the directory name.
	assert.Equal(t, "just-a-name", canonicalFestivalID("just-a-name"))
}
