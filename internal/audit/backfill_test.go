package audit

import (
	"testing"

	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
	"github.com/stretchr/testify/assert"
)

func TestCommitCoverageMatchesShortAndFullSHA(t *testing.T) {
	// Live capture stores a short sha; backfill derives the full sha for the same
	// commit. Coverage keys must match so backfill does not re-attach it (the
	// idempotence-critical case).
	live := []*ledgerkit.Event{{
		Kind:     ledgerkit.KindEvidenceAttached,
		Source:   ledgerkit.SourceCommand,
		Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "campaign-root", SHA: "89c5ad1"}},
	}}
	captured := capturedIndex(live)

	derived := DerivedFact{
		Kind:     ledgerkit.KindEvidenceAttached,
		Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "campaign-root", SHA: "89c5ad104ff798952e3499f9aab01071288b21b6"}},
	}
	assert.True(t, captured[factCoverageKey(derived)],
		"a full-sha backfill fact is covered by the live short-sha evidence")

	other := DerivedFact{
		Kind:     ledgerkit.KindEvidenceAttached,
		Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "campaign-root", SHA: "ffffffffffffffffffffffffffffffffffffffff"}},
	}
	assert.False(t, captured[factCoverageKey(other)], "a different commit is not covered")
}

func TestNormSHA(t *testing.T) {
	assert.Equal(t, "89c5ad1", normSHA("89c5ad104ff798952e3499f9aab01071288b21b6"))
	assert.Equal(t, "89c5ad1", normSHA("89c5ad1"))
	assert.Equal(t, "abc", normSHA("abc"))
}
