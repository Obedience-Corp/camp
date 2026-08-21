package audit

import (
	"context"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestCommitLogGitArgsMatchScanCoverage(t *testing.T) {
	joined := strings.Join(commitLogGitArgs, " ")
	assert.Contains(t, commitLogGitArgs, "--all",
		"backfill must walk every ref so ledger answers match git log --all")
	assert.NotContains(t, joined, "--no-merges",
		"backfill must include merge commits; --no-merges was the 13-vs-20 gap")
	assert.Equal(t, "log", commitLogGitArgs[0])
}

func TestCommitFactFromLogLine(t *testing.T) {
	const campaignID = "8deed8b4"
	const repo = "campaign-root"
	tests := []struct {
		name       string
		line       string
		wantOK     bool
		wantWI     string
		wantSHA    string
		wantAuthor string
	}{
		{
			name:       "doubled WI-WI- ref normalizes to WI-<hex>",
			line:       "aaa111\x1fLance\x1f2026-07-11T22:00:00Z\x1f[OBEY-CAMPAIGN-8deed8b4-WI-WI-25121c] feat: ledger path",
			wantOK:     true,
			wantWI:     "WI-25121c",
			wantSHA:    "aaa111",
			wantAuthor: "Lance",
		},
		{
			name:       "single-prefix WI ref is kept",
			line:       "bbb222\x1fLance\x1f2026-07-11T22:00:00Z\x1f[obey-campaign:8deed8b4-WI-25121c] feat: scan path",
			wantOK:     true,
			wantWI:     "WI-25121c",
			wantSHA:    "bbb222",
			wantAuthor: "Lance",
		},
		{
			name:   "untagged commit is not a fact",
			line:   "ccc333\x1fLance\x1f2026-07-11T22:00:00Z\x1ffix: no campaign tag",
			wantOK: false,
		},
		{
			name:   "malformed line is skipped",
			line:   "not-enough-fields",
			wantOK: false,
		},
		{
			name:   "empty sha is skipped",
			line:   "\x1fLance\x1f2026-07-11T22:00:00Z\x1f[obey-campaign:8deed8b4-WI-25121c] x",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := commitFactFromLogLine(campaignID, repo, tt.line)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			assert.Equal(t, tt.wantWI, got.Scope.Workitem)
			assert.Equal(t, campaignID, got.Scope.Campaign)
			require.Len(t, got.Evidence, 1)
			assert.Equal(t, tt.wantSHA, got.Evidence[0].SHA)
			assert.Equal(t, repo, got.Evidence[0].Repo)
			assert.Equal(t, tt.wantAuthor, got.Payload["author"])
			assert.Equal(t, "commit:"+repo+"@"+tt.wantSHA, got.IdentityKey)
		})
	}
}

func TestDeriveCommitFactsHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := deriveCommitFacts(ctx, "", "cid", []RepoTarget{{Label: "x", Path: "/nope"}})
	assert.Error(t, err)
}
