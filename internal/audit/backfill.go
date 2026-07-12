package audit

import (
	"bufio"
	"context"
	"os/exec"
	"strings"

	"github.com/Obedience-Corp/camp/pkg/commitkit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// BackfillResult reports a backfill run.
type BackfillResult struct {
	Derived int // facts derived from all source families
	Skipped int // facts already captured (live or a prior backfill)
	Events  []*ledgerkit.Event
}

// Backfill derives events from every source family (commit tags, intent
// frontmatter, festival status histories) and returns the source:backfill
// events for facts the ledger does not already capture. It is idempotent and
// live-wins: a fact captured live (a ULID event) or by a prior backfill (a bf_
// event) is skipped, so consecutive runs converge and live events are never
// duplicated. Emitted ids are content-derived (bf_ prefix).
func Backfill(ctx context.Context, campaignRoot, campaignID string, repos []RepoTarget) (*BackfillResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	facts, err := DeriveFacts(ctx, campaignRoot, campaignID)
	if err != nil {
		return nil, err
	}
	commitFacts, err := deriveCommitFacts(ctx, campaignRoot, campaignID, repos)
	if err != nil {
		return nil, err
	}
	facts = append(facts, commitFacts...)

	reader, err := ledgerkit.NewReader(campaignRoot)
	if err != nil {
		return nil, err
	}
	events, _, err := reader.Read(ctx)
	if err != nil {
		return nil, err
	}
	captured := capturedIndex(events)

	res := &BackfillResult{Derived: len(facts)}
	seen := make(map[string]bool)
	for _, f := range facts {
		if captured[factCoverageKey(f)] {
			res.Skipped++
			continue
		}
		id := ledgerkit.DerivedID("bf", f.IdentityKey)
		if seen[id] {
			continue // two derived facts with the same identity: keep one
		}
		seen[id] = true
		res.Events = append(res.Events, &ledgerkit.Event{
			V:        ledgerkit.EnvelopeVersion,
			ID:       id,
			TS:       f.TS,
			Kind:     f.Kind,
			Scope:    f.Scope,
			Actor:    ledgerkit.Actor{Type: ledgerkit.ActorUnknown},
			Why:      f.Why,
			Payload:  f.Payload,
			Evidence: f.Evidence,
			Source:   ledgerkit.SourceBackfill,
		})
	}
	return res, nil
}

// RepoTarget is a repo to scan for commit-tag facts, with its campaign-relative
// label.
type RepoTarget struct {
	Label string
	Path  string
}

// deriveCommitFacts turns tagged/degraded commits into evidence_attached facts,
// with scope from the tag. Untagged commits produce no fact (they are the
// bypass population the doctor reports, not backfillable to a scope).
func deriveCommitFacts(ctx context.Context, campaignRoot, campaignID string, repos []RepoTarget) ([]DerivedFact, error) {
	var facts []DerivedFact
	for _, r := range repos {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lines, err := commitLog(ctx, r.Path)
		if err != nil {
			continue // an unreadable/empty repo contributes nothing
		}
		for _, ln := range lines {
			cols := strings.SplitN(ln, "\x1f", 4)
			if len(cols) != 4 {
				continue
			}
			sha, author, date, subject := cols[0], cols[1], cols[2], cols[3]
			tc, _ := commitkit.ParseTagDetailed(subject)
			if tc.CampaignID == "" {
				continue // untagged: not attributable
			}
			facts = append(facts, DerivedFact{
				Kind: ledgerkit.KindEvidenceAttached,
				Scope: ledgerkit.Scope{
					Campaign: campaignID, Festival: tc.FestRef,
					Workitem: tc.WorkitemRef, Quest: tc.QuestID,
				},
				TS:          normalizeTS(date),
				Why:         firstLineOf(subject),
				Evidence:    []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: r.Label, SHA: sha}},
				Payload:     map[string]any{"author": author},
				IdentityKey: "commit:" + r.Label + "@" + sha,
			})
		}
	}
	return facts, nil
}

func commitLog(ctx context.Context, repoPath string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoPath, "log", "--no-merges", "--format=%H%x1f%an%x1f%aI%x1f%s").Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if sc.Text() != "" {
			lines = append(lines, sc.Text())
		}
	}
	return lines, sc.Err()
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
