package triage

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// AnchorCacheSchemaVersion is the format version of the remote anchor cache.
// A cache written under a different version is discarded rather than parsed,
// which is why a schema change never needs a migration here: the worst case is
// one extra round of remote checks.
const AnchorCacheSchemaVersion = "triage-anchor-cache/v1alpha1"

// AnchorCacheDir is the campaign-relative directory holding cached remote
// verdicts.
//
// Under `.campaign/cache/`, not under the run: a run is shared truth that
// belongs in git, while a cached PR state is a machine-local optimization that
// would only ever produce merge noise. Deleting this directory costs one round
// of API calls and nothing else.
const AnchorCacheDir = ".campaign/cache/triage/anchors"

// AnchorCacheFileName holds the pr verdicts.
const AnchorCacheFileName = "prs.json"

// PRObservation is one pull request as the remote reports it now.
type PRObservation struct {
	// State is lowercase, matching what an anchor records: open, closed,
	// merged.
	State string `json:"state"`
	// SHA is the merge commit when there is one.
	SHA string `json:"sha,omitempty"`
}

// RemoteChecker resolves pr anchors. The gh-backed implementation is one call
// per repo; tests inject a fake so the classification tables never touch a
// subprocess.
type RemoteChecker interface {
	// CheckPRs resolves every requested number in one repo. A number the
	// remote could not answer for is simply absent from the result, which the
	// caller records as unchecked rather than as a change.
	CheckPRs(ctx context.Context, repo string, numbers []int) (map[int]PRObservation, error)
}

// AnchorCache is the schema-versioned store of remote verdicts.
type AnchorCache struct {
	SchemaVersion string                     `json:"schema_version"`
	Entries       map[string]AnchorCacheItem `json:"entries"`
}

// AnchorCacheItem is one cached observation, keyed by the anchor's own string
// form so the key means something when a human reads the file.
type AnchorCacheItem struct {
	Observed  string    `json:"observed"`
	SHA       string    `json:"sha,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// fresh reports whether the entry still answers within the throttle window.
// A zero or negative throttle means every refresh calls out.
func (i AnchorCacheItem) fresh(now time.Time, throttle time.Duration) bool {
	if throttle <= 0 || i.CheckedAt.IsZero() {
		return false
	}
	return !i.CheckedAt.After(now) && now.Sub(i.CheckedAt) < throttle
}

// LoadAnchorCache reads the cache under campaignRoot.
//
// A missing, unreadable, corrupt, or foreign-schema cache all return an empty
// cache and no error. This is an optimization, and failing a refresh because a
// disposable file could not be parsed would trade a real capability for a
// worthless one.
func LoadAnchorCache(campaignRoot string) *AnchorCache {
	empty := &AnchorCache{
		SchemaVersion: AnchorCacheSchemaVersion,
		Entries:       map[string]AnchorCacheItem{},
	}
	body, err := os.ReadFile(AnchorCachePath(campaignRoot))
	if err != nil {
		return empty
	}
	var cache AnchorCache
	if err := json.Unmarshal(body, &cache); err != nil {
		return empty
	}
	if cache.SchemaVersion != AnchorCacheSchemaVersion {
		return empty
	}
	if cache.Entries == nil {
		cache.Entries = map[string]AnchorCacheItem{}
	}
	return &cache
}

// SaveAnchorCache writes the cache atomically.
func SaveAnchorCache(campaignRoot string, cache *AnchorCache) error {
	if cache == nil {
		return nil
	}
	cache.SchemaVersion = AnchorCacheSchemaVersion
	if cache.Entries == nil {
		cache.Entries = map[string]AnchorCacheItem{}
	}
	body, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "encoding the anchor cache")
	}
	body = append(body, '\n')

	path := AnchorCachePath(campaignRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return camperrors.Wrapf(err, "creating %s", filepath.Dir(path))
	}
	return writeAtomic(path, body)
}

// AnchorCachePath is where the remote verdicts live.
func AnchorCachePath(campaignRoot string) string {
	return filepath.Join(campaignRoot,
		filepath.FromSlash(AnchorCacheDir), AnchorCacheFileName)
}

// RemoteInput is one remote anchor pass.
type RemoteInput struct {
	CampaignRoot string
	// Checker resolves pull requests. Nil means no remote checking is
	// possible at all, and every pr anchor stays unchecked.
	Checker RemoteChecker
	Cache   *AnchorCache
	Now     time.Time
	// Throttle is how long a cached verdict answers before calling out again.
	Throttle time.Duration
}

// ResolveRemoteAnchors upgrades the unchecked remote entries in checks with
// observations from the cache and, where the cache is cold, the remote.
//
// It layers onto the local pass rather than replacing it, and that direction
// matters: CheckLocalAnchors has already recorded every pr anchor as
// unchecked-offline, so anything that goes wrong here — no gh, no auth, no
// network, a repo that errors — leaves the honest answer in place. The remote
// path can only ever improve on "we could not look"; it can never turn a
// failure into a guess.
//
// Returns the number of anchors it resolved, so a caller can report how much
// of the run was actually verified.
func ResolveRemoteAnchors(ctx context.Context, checks map[string][]AnchorCheck, in RemoteInput) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if in.Cache == nil {
		in.Cache = &AnchorCache{Entries: map[string]AnchorCacheItem{}}
	}
	if in.Cache.Entries == nil {
		in.Cache.Entries = map[string]AnchorCacheItem{}
	}

	pending := pendingPRs(checks, in)
	fetched, cacheChanged := fetchPending(ctx, pending, in)

	resolved := 0
	for _, rowChecks := range checks {
		for i := range rowChecks {
			anchor := rowChecks[i].Anchor
			if anchor.Kind != AnchorKindPR {
				continue
			}
			item, ok := lookupObservation(anchor, in, fetched)
			if !ok {
				continue
			}
			rowChecks[i].Observed = item.Observed
			rowChecks[i].Unchecked = false
			resolved++
		}
	}

	if cacheChanged && in.CampaignRoot != "" {
		if err := SaveAnchorCache(in.CampaignRoot, in.Cache); err != nil {
			// A cache that will not persist costs the next run some API
			// calls. It does not make this refresh's answers wrong, so it
			// must not fail the refresh.
			return resolved, nil
		}
	}
	return resolved, nil
}

// lookupObservation finds an anchor's observation in this pass's fetches or in
// a cache entry still inside the throttle window.
func lookupObservation(anchor Anchor, in RemoteInput, fetched map[string]AnchorCacheItem) (AnchorCacheItem, bool) {
	key := anchor.String()
	if item, ok := fetched[key]; ok {
		return item, true
	}
	if item, ok := in.Cache.Entries[key]; ok && item.fresh(in.Now, in.Throttle) {
		return item, true
	}
	return AnchorCacheItem{}, false
}

// pendingPRs groups the pr anchors that need a remote call by repo, skipping
// any the cache can still answer. Numbers come back sorted and deduplicated so
// one repo makes exactly one call with a stable argument list.
func pendingPRs(checks map[string][]AnchorCheck, in RemoteInput) map[string][]int {
	if in.Checker == nil {
		return nil
	}
	byRepo := make(map[string]map[int]bool)
	for _, rowChecks := range checks {
		for _, check := range rowChecks {
			anchor := check.Anchor
			if anchor.Kind != AnchorKindPR || anchor.Repo == "" || anchor.Number == 0 {
				continue
			}
			if item, ok := in.Cache.Entries[anchor.String()]; ok && item.fresh(in.Now, in.Throttle) {
				continue
			}
			if byRepo[anchor.Repo] == nil {
				byRepo[anchor.Repo] = map[int]bool{}
			}
			byRepo[anchor.Repo][anchor.Number] = true
		}
	}

	out := make(map[string][]int, len(byRepo))
	for repo, numbers := range byRepo {
		list := make([]int, 0, len(numbers))
		for n := range numbers {
			list = append(list, n)
		}
		sort.Ints(list)
		out[repo] = list
	}
	return out
}

// fetchPending calls the remote once per repo and folds the results into the
// cache. A repo that errors is skipped: its anchors keep the unchecked answer
// the local pass recorded.
func fetchPending(ctx context.Context, pending map[string][]int, in RemoteInput) (map[string]AnchorCacheItem, bool) {
	fetched := make(map[string]AnchorCacheItem)
	changed := false

	for _, repo := range sortedKeys(pending) {
		if ctx.Err() != nil {
			return fetched, changed
		}
		observations, err := in.Checker.CheckPRs(ctx, repo, pending[repo])
		if err != nil {
			continue
		}
		for number, observed := range observations {
			key := (Anchor{Kind: AnchorKindPR, Repo: repo, Number: number}).String()
			item := AnchorCacheItem{
				Observed:  observed.State,
				SHA:       observed.SHA,
				CheckedAt: in.Now,
			}
			fetched[key] = item
			in.Cache.Entries[key] = item
			changed = true
		}
	}
	return fetched, changed
}

// GHRemoteChecker resolves pull requests through the gh CLI.
type GHRemoteChecker struct {
	ghPath string
}

// NewGHRemoteChecker locates gh on PATH.
//
// A missing gh is not an error the caller has to handle specially — it is the
// offline case, and the caller passes a nil RemoteChecker so every pr anchor
// records unchecked-offline. Returning the error lets a caller that wants to
// say why choose to.
func NewGHRemoteChecker() (*GHRemoteChecker, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return nil, camperrors.Wrap(err,
			"gh CLI not found; install it from https://cli.github.com to re-check pr anchors")
	}
	return &GHRemoteChecker{ghPath: path}, nil
}

// ghGraphQLResponse is the shape of one batched query.
type ghGraphQLResponse struct {
	Data struct {
		Repository map[string]struct {
			State       string `json:"state"`
			MergeCommit *struct {
				OID string `json:"oid"`
			} `json:"mergeCommit"`
		} `json:"repository"`
	} `json:"data"`
}

// CheckPRs resolves every number in one GraphQL call.
//
// One call per repo rather than one per PR: spec doc 04 budgets "a handful of
// batched remote calls" for a 500-row run, and the REST endpoint is per pull
// request. Aliased GraphQL fields are what make the batch expressible.
func (c *GHRemoteChecker) CheckPRs(ctx context.Context, repo string, numbers []int) (map[int]PRObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, camperrors.NewValidation("repo",
			"must be owner/name, got "+quote(repo), camperrors.ErrInvalidInput)
	}
	if len(numbers) == 0 {
		return map[int]PRObservation{}, nil
	}

	cmd := exec.CommandContext(ctx, c.ghPath, "api", "graphql",
		"-f", "query="+prBatchQuery(owner, name, numbers))
	out, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "gh api graphql for %s", repo)
	}

	var parsed ghGraphQLResponse
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, camperrors.Wrapf(err, "parsing gh output for %s", repo)
	}

	observations := make(map[int]PRObservation, len(numbers))
	for alias, node := range parsed.Data.Repository {
		number, err := strconv.Atoi(strings.TrimPrefix(alias, prAliasPrefix))
		if err != nil {
			continue
		}
		observation := PRObservation{State: strings.ToLower(node.State)}
		if node.MergeCommit != nil {
			observation.SHA = node.MergeCommit.OID
		}
		observations[number] = observation
	}
	return observations, nil
}

// prAliasPrefix names the GraphQL alias each pull request is queried under.
// The number is carried in the alias because a GraphQL response is an object,
// not an ordered list, so the alias is the only way back to which PR is which.
const prAliasPrefix = "pr"

// prBatchQuery builds one aliased query for every requested number.
func prBatchQuery(owner, name string, numbers []int) string {
	var b strings.Builder
	b.WriteString(`query { repository(owner: "`)
	b.WriteString(owner)
	b.WriteString(`", name: "`)
	b.WriteString(name)
	b.WriteString(`") {`)
	for _, number := range numbers {
		b.WriteString(" " + prAliasPrefix + strconv.Itoa(number))
		b.WriteString(": pullRequest(number: ")
		b.WriteString(strconv.Itoa(number))
		b.WriteString(") { state mergeCommit { oid } }")
	}
	b.WriteString(" } }")
	return b.String()
}
