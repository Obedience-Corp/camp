package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// EntryKind distinguishes toggle sources from destination visits in the nav log.
type EntryKind string

const (
	// KindToggle is a camp go source. GetLastLocation reads these.
	KindToggle EntryKind = "toggle"
	// KindVisit is a destination or other cheap, correct activity used for ranking.
	KindVisit EntryKind = "visit"
)

// kindKey returns the dedup kind for an entry. Empty kind (legacy lines) is toggle.
func kindKey(e NavigationEntry) EntryKind {
	if e.Kind == "" {
		return KindToggle
	}
	return e.Kind
}

func normRel(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	p = strings.Trim(p, "/")
	if p == "." {
		return ""
	}
	return p
}

func hasDotDot(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func deriveRelString(campaignRoot, abs string) string {
	if abs == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Clean(campaignRoot), filepath.Clean(abs))
	if err != nil {
		return ""
	}
	return normRel(rel)
}

func deriveRelWrite(campaignRoot, abs string) string {
	resolved, err := pathutil.ResolveRoot(campaignRoot)
	if err != nil {
		return deriveRelString(campaignRoot, abs)
	}
	rel, err := pathutil.RelativeToRoot(resolved, abs)
	if err != nil {
		return deriveRelString(campaignRoot, abs)
	}
	return normRel(rel)
}

func fillEntry(campaignRoot string, e NavigationEntry) NavigationEntry {
	if e.Rel == "" && e.Location != "" {
		e.Rel = deriveRelString(campaignRoot, e.Location)
	}
	e.Rel = normRel(e.Rel)
	if e.Kind == "" {
		e.Kind = KindToggle
	}
	return e
}

func backfill(campaignRoot string, entries []NavigationEntry) []NavigationEntry {
	out := make([]NavigationEntry, len(entries))
	for i, e := range entries {
		out[i] = fillEntry(campaignRoot, e)
	}
	return out
}

// appendUnique inserts e, collapsing an older row with the same (rel, kind).
// KindVisit rows with empty, ".", or ".." rels are dropped.
func appendUnique(entries []NavigationEntry, e NavigationEntry) []NavigationEntry {
	e.Rel = normRel(e.Rel)
	if e.Kind == KindVisit && (e.Rel == "" || hasDotDot(e.Rel)) {
		return entries
	}
	out := make([]NavigationEntry, 0, len(entries)+1)
	for _, existing := range entries {
		if existing.Rel == e.Rel && kindKey(existing) == kindKey(e) {
			continue
		}
		out = append(out, existing)
	}
	out = append(out, e)
	if len(out) > maxUniqueEntries {
		out = out[len(out)-maxUniqueEntries:]
	}
	return out
}

// pathMatches reports whether visitRel should rank itemPath.
// itemPath "projects/camp" matches rel "projects/camp" and "projects/camp/cmd",
// not "projects/camp-timeline".
func pathMatches(visitRel, itemPath string) bool {
	rel, path := normRel(visitRel), normRel(itemPath)
	if rel == "" || path == "" {
		return false
	}
	return rel == path || strings.HasPrefix(rel, path+"/")
}

// RankMap is last timestamp per normalized campaign-relative path.
// Both toggle and visit rows contribute. Empty / "." / ".." keys are omitted.
// RankMap does not Stat or EvalSymlinks.
func RankMap(entries []NavigationEntry, campaignRoot string) map[string]time.Time {
	ranks := make(map[string]time.Time, len(entries))
	for _, e := range entries {
		rel := e.Rel
		if rel == "" {
			rel = deriveRelString(campaignRoot, e.Location)
		}
		rel = normRel(rel)
		if rel == "" || hasDotDot(rel) {
			continue
		}
		if ts, ok := ranks[rel]; !ok || e.Time.After(ts) {
			ranks[rel] = e.Time
		}
	}
	return ranks
}

// RankTime returns the best timestamp that pathMatches itemRel, or zero.
func RankTime(ranks map[string]time.Time, itemRel string) time.Time {
	var best time.Time
	for rel, ts := range ranks {
		if pathMatches(rel, itemRel) && ts.After(best) {
			best = ts
		}
	}
	return best
}

// RecordVisit appends a destination visit. It does not Stat.
// Empty / "." / ".." rels are dropped (no error).
func RecordVisit(ctx context.Context, campaignRoot, location string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rel := deriveRelWrite(campaignRoot, location)
	if rel == "" || hasDotDot(rel) {
		return nil
	}
	return SaveEntry(ctx, campaignRoot, NavigationEntry{
		Location: location,
		Rel:      rel,
		Time:     time.Now(),
		Kind:     KindVisit,
	})
}

// RecordJump writes a source toggle and, when dest ranks as a distinct
// in-camp path, a visit one nanosecond later, in one atomic rewrite.
func RecordJump(ctx context.Context, campaignRoot, srcAbs, destAbs string) error {
	return recordJumpAt(ctx, campaignRoot, srcAbs, destAbs, time.Now())
}

func recordJumpAt(ctx context.Context, campaignRoot, srcAbs, destAbs string, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if info, err := os.Stat(srcAbs); err != nil || !info.IsDir() {
		return camperrors.Newf("invalid location: %s does not exist or is not a directory", srcAbs)
	}
	toggle := NavigationEntry{
		Location: srcAbs,
		Rel:      deriveRelWrite(campaignRoot, srcAbs),
		Time:     now,
		Kind:     KindToggle,
	}
	destRel := deriveRelWrite(campaignRoot, destAbs)
	var visit *NavigationEntry
	if destAbs != "" && destRel != "" && destRel != toggle.Rel && !hasDotDot(destRel) {
		visit = &NavigationEntry{
			Location: destAbs,
			Rel:      destRel,
			Time:     now.Add(time.Nanosecond),
			Kind:     KindVisit,
		}
	}
	return rewriteHistory(ctx, campaignRoot, func(entries []NavigationEntry) []NavigationEntry {
		entries = appendUnique(entries, toggle)
		if visit != nil {
			entries = appendUnique(entries, *visit)
		}
		return entries
	})
}
