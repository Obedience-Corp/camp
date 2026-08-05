package triage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// filepathDir is filepath.Dir, aliased so the import list stays short in a
// file that otherwise never touches path building.
func filepathDir(p string) string { return filepath.Dir(p) }

// fakeRemote records what it was asked and answers from a fixed table.
type fakeRemote struct {
	// byRepo is the state each pull request reports.
	byRepo map[string]map[int]PRObservation
	// err is returned for any repo named here.
	err map[string]bool
	// calls records one entry per CheckPRs invocation.
	calls []remoteCall
}

type remoteCall struct {
	repo    string
	numbers []int
}

func (f *fakeRemote) CheckPRs(_ context.Context, repo string, numbers []int) (map[int]PRObservation, error) {
	f.calls = append(f.calls, remoteCall{repo: repo, numbers: numbers})
	if f.err[repo] {
		return nil, camperrors.New("remote unavailable")
	}
	return f.byRepo[repo], nil
}

func prAnchor(repo string, number int, observed string) Anchor {
	return Anchor{Kind: AnchorKindPR, Repo: repo, Number: number, Observed: observed}
}

// localChecks is what CheckLocalAnchors leaves behind for pr anchors: the
// honest unchecked answer the remote pass layers onto.
func localChecks(t *testing.T, anchors map[string][]Anchor) map[string][]AnchorCheck {
	t.Helper()
	checks, err := CheckLocalAnchors(context.Background(), t.TempDir(), anchors, DiscoveryIndex{})
	require.NoError(t, err)
	return checks
}

// TestResolveRemoteAnchorsUpgradesTheUncheckedAnswer is the core of the remote
// pass: a pr anchor starts unchecked and only becomes checked when something
// actually answered.
func TestResolveRemoteAnchorsUpgradesTheUncheckedAnswer(t *testing.T) {
	anchors := map[string][]Anchor{
		"row": {prAnchor("Obedience-Corp/camp", 546, "open")},
	}
	checks := localChecks(t, anchors)
	require.True(t, checks["row"][0].Unchecked, "the local pass leaves pr unchecked")

	remote := &fakeRemote{byRepo: map[string]map[int]PRObservation{
		"Obedience-Corp/camp": {546: {State: "merged", SHA: "abc123"}},
	}}

	resolved, err := ResolveRemoteAnchors(context.Background(), checks, RemoteInput{
		CampaignRoot: t.TempDir(), Checker: remote,
		Now: testAt, Throttle: 5 * time.Minute,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, resolved)
	assert.False(t, checks["row"][0].Unchecked)
	assert.Equal(t, "merged", checks["row"][0].Observed)
	assert.False(t, checks["row"][0].Matches(), "open -> merged is a change")
}

// TestResolveRemoteAnchorsWithoutACheckerStaysUnchecked is the offline case:
// no gh, no guess.
func TestResolveRemoteAnchorsWithoutACheckerStaysUnchecked(t *testing.T) {
	checks := localChecks(t, map[string][]Anchor{
		"row": {prAnchor("Obedience-Corp/camp", 546, "open")},
	})

	resolved, err := ResolveRemoteAnchors(context.Background(), checks, RemoteInput{
		CampaignRoot: t.TempDir(), Checker: nil, Now: testAt,
	})
	require.NoError(t, err)

	assert.Equal(t, 0, resolved)
	assert.True(t, checks["row"][0].Unchecked)
	assert.Equal(t, ObservedUncheckedOffline, checks["row"][0].Observed)
}

// TestResolveRemoteAnchorsSurvivesAFailingRepo: a repo that errors must not
// fail the refresh or invalidate its rows.
func TestResolveRemoteAnchorsSurvivesAFailingRepo(t *testing.T) {
	checks := localChecks(t, map[string][]Anchor{
		"row-a": {prAnchor("Obedience-Corp/camp", 1, "open")},
		"row-b": {prAnchor("Obedience-Corp/fest", 2, "open")},
	})

	remote := &fakeRemote{
		byRepo: map[string]map[int]PRObservation{
			"Obedience-Corp/fest": {2: {State: "merged"}},
		},
		err: map[string]bool{"Obedience-Corp/camp": true},
	}

	resolved, err := ResolveRemoteAnchors(context.Background(), checks, RemoteInput{
		CampaignRoot: t.TempDir(), Checker: remote, Now: testAt,
	})
	require.NoError(t, err, "one bad repo must not fail the whole refresh")

	assert.Equal(t, 1, resolved)
	assert.True(t, checks["row-a"][0].Unchecked, "the failing repo keeps the honest answer")
	assert.False(t, checks["row-b"][0].Unchecked)
}

// TestResolveRemoteAnchorsBatchesOneCallPerRepo pins the cost bound spec doc
// 04 sets: a handful of batched calls, not one per pull request.
func TestResolveRemoteAnchorsBatchesOneCallPerRepo(t *testing.T) {
	checks := localChecks(t, map[string][]Anchor{
		"row-a": {prAnchor("o/camp", 3, "open"), prAnchor("o/camp", 1, "open")},
		"row-b": {prAnchor("o/camp", 2, "open")},
		"row-c": {prAnchor("o/fest", 9, "open")},
		// The same PR anchored twice must not become two requests.
		"row-d": {prAnchor("o/camp", 1, "open")},
	})

	remote := &fakeRemote{byRepo: map[string]map[int]PRObservation{}}
	_, err := ResolveRemoteAnchors(context.Background(), checks, RemoteInput{
		CampaignRoot: t.TempDir(), Checker: remote, Now: testAt,
	})
	require.NoError(t, err)

	require.Len(t, remote.calls, 2, "one call per repo")
	assert.Equal(t, "o/camp", remote.calls[0].repo)
	assert.Equal(t, []int{1, 2, 3}, remote.calls[0].numbers,
		"numbers are deduplicated and sorted so the call is reproducible")
	assert.Equal(t, "o/fest", remote.calls[1].repo)
	assert.Equal(t, []int{9}, remote.calls[1].numbers)
}

// TestResolveRemoteAnchorsAnswersFromCacheInsideTheWindow covers the throttle.
func TestResolveRemoteAnchorsAnswersFromCacheInsideTheWindow(t *testing.T) {
	anchor := prAnchor("o/camp", 546, "open")
	cache := &AnchorCache{
		SchemaVersion: AnchorCacheSchemaVersion,
		Entries: map[string]AnchorCacheItem{
			anchor.String(): {Observed: "merged", CheckedAt: testAt.Add(-time.Minute)},
		},
	}

	checks := localChecks(t, map[string][]Anchor{"row": {anchor}})
	remote := &fakeRemote{byRepo: map[string]map[int]PRObservation{}}

	resolved, err := ResolveRemoteAnchors(context.Background(), checks, RemoteInput{
		CampaignRoot: t.TempDir(), Checker: remote, Cache: cache,
		Now: testAt, Throttle: 5 * time.Minute,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, resolved)
	assert.Equal(t, "merged", checks["row"][0].Observed)
	assert.Empty(t, remote.calls, "a cached verdict inside the window answers without a call")
}

// TestResolveRemoteAnchorsRechecksOutsideTheWindow is the other half: a stale
// cache entry does not answer.
func TestResolveRemoteAnchorsRechecksOutsideTheWindow(t *testing.T) {
	anchor := prAnchor("o/camp", 546, "open")
	cache := &AnchorCache{
		SchemaVersion: AnchorCacheSchemaVersion,
		Entries: map[string]AnchorCacheItem{
			anchor.String(): {Observed: "open", CheckedAt: testAt.Add(-time.Hour)},
		},
	}

	checks := localChecks(t, map[string][]Anchor{"row": {anchor}})
	remote := &fakeRemote{byRepo: map[string]map[int]PRObservation{
		"o/camp": {546: {State: "merged"}},
	}}

	_, err := ResolveRemoteAnchors(context.Background(), checks, RemoteInput{
		CampaignRoot: t.TempDir(), Checker: remote, Cache: cache,
		Now: testAt, Throttle: 5 * time.Minute,
	})
	require.NoError(t, err)

	require.Len(t, remote.calls, 1)
	assert.Equal(t, "merged", checks["row"][0].Observed)
}

// TestResolveRemoteAnchorsZeroThrottleAlwaysChecks: a campaign that wants
// every refresh to hit the network sets the interval to zero.
func TestResolveRemoteAnchorsZeroThrottleAlwaysChecks(t *testing.T) {
	anchor := prAnchor("o/camp", 546, "open")
	cache := &AnchorCache{
		SchemaVersion: AnchorCacheSchemaVersion,
		Entries: map[string]AnchorCacheItem{
			anchor.String(): {Observed: "open", CheckedAt: testAt},
		},
	}

	checks := localChecks(t, map[string][]Anchor{"row": {anchor}})
	remote := &fakeRemote{byRepo: map[string]map[int]PRObservation{
		"o/camp": {546: {State: "merged"}},
	}}

	_, err := ResolveRemoteAnchors(context.Background(), checks, RemoteInput{
		CampaignRoot: t.TempDir(), Checker: remote, Cache: cache,
		Now: testAt, Throttle: 0,
	})
	require.NoError(t, err)

	require.Len(t, remote.calls, 1, "a zero throttle means never cache")
	assert.Equal(t, "merged", checks["row"][0].Observed)
}

// TestAnchorCacheRoundTrip covers persistence, including the atomic write and
// the machine-local location.
func TestAnchorCacheRoundTrip(t *testing.T) {
	root := t.TempDir()
	anchor := prAnchor("o/camp", 546, "open")

	checks := localChecks(t, map[string][]Anchor{"row": {anchor}})
	_, err := ResolveRemoteAnchors(context.Background(), checks, RemoteInput{
		CampaignRoot: root,
		Checker: &fakeRemote{byRepo: map[string]map[int]PRObservation{
			"o/camp": {546: {State: "merged", SHA: "abc123"}},
		}},
		Now: testAt, Throttle: 5 * time.Minute,
	})
	require.NoError(t, err)

	body, err := os.ReadFile(AnchorCachePath(root))
	require.NoError(t, err, "the cache persists under .campaign/cache, not in the run")
	assert.Contains(t, string(body), AnchorCacheSchemaVersion)

	reloaded := LoadAnchorCache(root)
	item, ok := reloaded.Entries[anchor.String()]
	require.True(t, ok)
	assert.Equal(t, "merged", item.Observed)
	assert.Equal(t, "abc123", item.SHA)
	assert.Equal(t, testAt, item.CheckedAt.UTC())
}

// TestLoadAnchorCacheDegradesToEmpty: the cache is disposable, so nothing
// about it may fail a refresh.
func TestLoadAnchorCacheDegradesToEmpty(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "corrupt json", body: "{not json"},
		{name: "foreign schema", body: `{"schema_version":"something-else/v9","entries":{}}`},
		{name: "null entries", body: `{"schema_version":"` + AnchorCacheSchemaVersion + `","entries":null}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepathDir(AnchorCachePath(root)), 0o755))
			require.NoError(t, os.WriteFile(AnchorCachePath(root), []byte(tt.body), 0o644))

			cache := LoadAnchorCache(root)
			require.NotNil(t, cache)
			assert.NotNil(t, cache.Entries, "a usable empty cache, never a nil map")
			assert.Empty(t, cache.Entries)
		})
	}
}

// TestLoadAnchorCacheMissingFile is the first-run case.
func TestLoadAnchorCacheMissingFile(t *testing.T) {
	cache := LoadAnchorCache(t.TempDir())
	require.NotNil(t, cache)
	assert.Empty(t, cache.Entries)
	assert.Equal(t, AnchorCacheSchemaVersion, cache.SchemaVersion)
}

// TestPRBatchQueryAliasesEveryNumber pins the batching mechanism: a GraphQL
// response is an object, so the alias is the only route back to which PR is
// which.
func TestPRBatchQueryAliasesEveryNumber(t *testing.T) {
	query := prBatchQuery("Obedience-Corp", "camp", []int{546, 12})

	assert.Contains(t, query, `repository(owner: "Obedience-Corp", name: "camp")`)
	assert.Contains(t, query, "pr546: pullRequest(number: 546)")
	assert.Contains(t, query, "pr12: pullRequest(number: 12)")
	assert.Contains(t, query, "state mergeCommit { oid }",
		"the merge SHA comes back in the same call as the state")
}

// TestGHResponseParsesStatesAndSHAs covers the parse of a real gh payload
// shape, including the lowercase normalization anchors record.
func TestGHResponseParsesStatesAndSHAs(t *testing.T) {
	raw := `{"data":{"repository":{
		"pr546":{"state":"MERGED","mergeCommit":{"oid":"abc123"}},
		"pr12":{"state":"OPEN","mergeCommit":null}
	}}}`

	var parsed ghGraphQLResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &parsed))

	merged := parsed.Data.Repository["pr546"]
	assert.Equal(t, "MERGED", merged.State)
	require.NotNil(t, merged.MergeCommit)
	assert.Equal(t, "abc123", merged.MergeCommit.OID)

	open := parsed.Data.Repository["pr12"]
	assert.Nil(t, open.MergeCommit, "an unmerged PR has no merge commit")
}

// TestCheckPRsRejectsAMalformedRepo keeps a bad repo out of the query rather
// than sending it and reading whatever comes back.
func TestCheckPRsRejectsAMalformedRepo(t *testing.T) {
	checker := &GHRemoteChecker{ghPath: "/nonexistent/gh"}
	_, err := checker.CheckPRs(context.Background(), "not-owner-slash-name", []int{1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner/name")
}

// TestCheckPRsWithNoNumbersMakesNoCall avoids an empty query.
func TestCheckPRsWithNoNumbersMakesNoCall(t *testing.T) {
	checker := &GHRemoteChecker{ghPath: "/nonexistent/gh"}
	got, err := checker.CheckPRs(context.Background(), "o/camp", nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestResolveRemoteAnchorsHonorsCancellation is the context rule at the
// network boundary.
func TestResolveRemoteAnchorsHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ResolveRemoteAnchors(ctx, map[string][]AnchorCheck{}, RemoteInput{Now: testAt})
	assert.ErrorIs(t, err, context.Canceled)
}
