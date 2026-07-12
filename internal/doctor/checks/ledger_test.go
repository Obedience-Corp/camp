package checks

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/doctor"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// severityOf returns the first issue whose Details "type" matches, for asserting
// each diagnostic maps to the expected severity.
func issueByType(issues []doctor.Issue, typ string) (doctor.Issue, bool) {
	for _, i := range issues {
		if i.Details["type"] == typ {
			return i, true
		}
	}
	return doctor.Issue{}, false
}

func TestLedgerCheckBuildIssues(t *testing.T) {
	// Injected resolver: repo "gone" is missing, sha "bad" is dangling, else ok.
	resolve := func(_ context.Context, _ string, repo, sha string) evidenceStatus {
		switch {
		case repo == "gone":
			return evidenceRepoMissing
		case sha == "bad":
			return evidenceShaMissing
		default:
			return evidenceOK
		}
	}
	c := &LedgerCheck{resolve: resolve}

	diag := &ledgerkit.Diagnostics{
		EventCount:      3,
		Skipped:         []ledgerkit.SkippedLine{{Shard: "s.jsonl", Line: 4, Reason: "invalid json"}},
		UnknownVersions: 2,
		Duplicates:      []ledgerkit.Duplicate{{ID: "dup", Locations: []string{"a:1", "b:2"}}},
		ShardViolations: []ledgerkit.ShardViolation{{Shard: "bad/w.jsonl", Reason: "month directory \"bad\" is not a valid YYYY-MM"}},
		Events: []*ledgerkit.Event{
			{ID: "e1", Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "gone", SHA: "abc"}}},
			{ID: "e2", Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "camp", SHA: "bad"}}},
			{ID: "e3", Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "camp", SHA: "good"}}},
		},
	}
	issues := c.buildIssues(context.Background(), "/camp", diag)

	tests := []struct {
		typ      string
		severity doctor.Severity
	}{
		{"malformed_line", doctor.SeverityWarning},
		{"unknown_version", doctor.SeverityInfo}, // A3: newer version is info, not error
		{"duplicate_id", doctor.SeverityWarning},
		{"shard_naming", doctor.SeverityWarning},
		{"evidence_repo_missing", doctor.SeverityInfo},
		{"evidence_dangling", doctor.SeverityWarning},
	}
	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			issue, ok := issueByType(issues, tt.typ)
			require.True(t, ok, "expected an issue of type %s", tt.typ)
			assert.Equal(t, tt.severity, issue.Severity)
			assert.Equal(t, "ledger", issue.CheckID)
		})
	}

	// The resolvable evidence (repo camp, sha good) produces no issue.
	for _, i := range issues {
		assert.NotEqual(t, "e3", i.Details["event"], "resolvable evidence must not produce an issue")
	}
	// Ledger findings are never errors, so the check never fails camp doctor.
	assert.False(t, anyError(issues))
}

func TestLedgerCheckCleanLedgerNoIssues(t *testing.T) {
	c := &LedgerCheck{resolve: func(context.Context, string, string, string) evidenceStatus { return evidenceOK }}
	issues := c.buildIssues(context.Background(), "/camp", &ledgerkit.Diagnostics{EventCount: 0})
	assert.Empty(t, issues)
}

func TestLedgerCheckMetadata(t *testing.T) {
	c := NewLedgerCheck()
	assert.Equal(t, "ledger", c.ID())
	assert.NotEmpty(t, c.Name())
	assert.NotEmpty(t, c.Description())
	// Fix is a no-op for informational ledger findings.
	fixed, err := c.Fix(context.Background(), "/camp", []doctor.Issue{{CheckID: "ledger"}})
	require.NoError(t, err)
	assert.Empty(t, fixed)
}

func TestGitResolveCommitRepoPath(t *testing.T) {
	// Pure path-mapping check via the same rules as gitResolveCommit (without
	// requiring a git object). Slash labels join to campaign root; bare names
	// go under projects/.
	root := "/campaign"
	cases := []struct {
		repo string
		want string
	}{
		{"", root},
		{"campaign-root", root},
		{".", root},
		{"projects/camp", filepath.Join(root, "projects", "camp")},
		{"camp", filepath.Join(root, "projects", "camp")}, // legacy bare label
	}
	for _, tc := range cases {
		got := resolveRepoPath(root, tc.repo)
		assert.Equal(t, tc.want, got, "repo=%q", tc.repo)
	}
}
