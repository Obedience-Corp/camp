package leverage

import (
	"context"
	"strings"
	"time"
	"unicode"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// AuthorMatch is the result of expanding an --author filter against authors.json
// and free-form email/name substrings.
type AuthorMatch struct {
	// Filter is the original user-provided filter string.
	Filter string

	// AuthorIDs are canonical resolver IDs matched by the filter.
	// When non-empty, matching is identity-group exclusive (no raw substring).
	AuthorIDs map[string]bool

	// Emails are concrete addresses for git log --author= queries.
	// Populated from configured identity groups, or the raw filter when no
	// configured identity matched.
	Emails []string

	// Configured is true when at least one authors.json identity matched.
	// When true, the raw filter is not used as a git substring (avoids
	// collisions like "alice" matching "malice").
	Configured bool
}

// ExpandAuthorFilter resolves an --author flag against the author config.
//
// Matching is case-insensitive substring against author ID, display name, and
// configured emails. When a configured identity matches, all of its emails are
// included and the raw filter is NOT retained as a git substring.
//
// When nothing in the config matches, the raw filter is used as a single git
// author substring (ad-hoc email / name search).
func ExpandAuthorFilter(resolver *AuthorResolver, filter string) AuthorMatch {
	filter = strings.TrimSpace(filter)
	m := AuthorMatch{
		Filter:    filter,
		AuthorIDs: make(map[string]bool),
	}
	if filter == "" {
		return m
	}

	lower := strings.ToLower(filter)
	seenEmail := make(map[string]bool)

	addEmail := func(email string) {
		email = strings.TrimSpace(email)
		if email == "" {
			return
		}
		key := strings.ToLower(email)
		if seenEmail[key] {
			return
		}
		seenEmail[key] = true
		m.Emails = append(m.Emails, email)
	}

	if resolver != nil && resolver.config != nil {
		for id, identity := range resolver.config.Authors {
			if identity.Exclude {
				continue
			}
			// Avoid naive substring matching on IDs/emails (e.g. "alice" must
			// not select "malice"). Use exact ID/email/local-part and word-wise
			// display-name matching instead.
			if !identityMatchesFilter(id, identity, lower) {
				continue
			}
			m.Configured = true
			m.AuthorIDs[id] = true
			for _, email := range identity.Emails {
				addEmail(email)
			}
		}
	}

	if m.Configured {
		// Identity-group match: use only group emails / IDs. Do not append the
		// raw filter as a git substring (would include "malice" for "alice").
		return m
	}

	// No configured identity: raw filter is the sole git author query.
	addEmail(filter)

	// Seed AuthorIDs from resolver so ownership lookups work for exact emails
	// that are not grouped. Skip excluded identities.
	if resolver != nil && !resolver.IsExcluded(filter) {
		id := resolver.Resolve(filter)
		if id != "" {
			m.AuthorIDs[id] = true
		}
	}

	return m
}

// identityMatchesFilter reports whether a configured identity matches filter
// (already lowercased). Matching is intentionally stricter than raw git
// --author substring so short filters do not collide across identities.
func identityMatchesFilter(id string, identity AuthorIdentity, filterLower string) bool {
	if filterLower == "" {
		return false
	}
	if strings.ToLower(id) == filterLower {
		return true
	}
	if containsWord(strings.ToLower(identity.DisplayName), filterLower) {
		return true
	}
	for _, email := range identity.Emails {
		el := strings.ToLower(strings.TrimSpace(email))
		if el == filterLower {
			return true
		}
		// Local-part exact: "alice" matches alice@example.com, not malice@…
		if at := strings.IndexByte(el, '@'); at > 0 {
			local := el[:at]
			if local == filterLower {
				return true
			}
		}
	}
	return false
}

// containsWord reports whether word appears as a whole alphanumeric token in text.
func containsWord(text, word string) bool {
	if text == "" || word == "" {
		return false
	}
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, f := range fields {
		if f == word {
			return true
		}
	}
	return false
}

// MatchesAuthor reports whether email belongs to the matched personal filter set.
//
// When a configured identity matched (or AuthorIDs is non-empty from resolve),
// membership is by canonical author ID only — never by raw substring on the
// filter string.
func (m AuthorMatch) MatchesAuthor(resolver *AuthorResolver, email string) bool {
	if m.Filter == "" {
		return false
	}

	if len(m.AuthorIDs) > 0 {
		if resolver != nil {
			return m.AuthorIDs[resolver.Resolve(email)]
		}
		// No resolver: exact email match against the expanded email list.
		for _, e := range m.Emails {
			if strings.EqualFold(e, email) {
				return true
			}
		}
		return false
	}

	// Ad-hoc filter with no IDs: substring match on email (legacy git behavior).
	lowerEmail := strings.ToLower(strings.TrimSpace(email))
	lowerFilter := strings.ToLower(m.Filter)
	return strings.Contains(lowerEmail, lowerFilter)
}

// AuthorHasCommitsMatch reports whether any matched identity has commits in gitDir.
func AuthorHasCommitsMatch(ctx context.Context, gitDir string, match AuthorMatch) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	queries := match.Emails
	if len(queries) == 0 && match.Filter != "" {
		queries = []string{match.Filter}
	}
	for _, q := range queries {
		ok, err := AuthorHasCommits(ctx, gitDir, q)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// isNoAuthorCommits reports whether err is the expected "no commits for author"
// outcome (as opposed to cancellation or git operational failure).
func isNoAuthorCommits(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no commits by ") ||
		strings.Contains(msg, "no commits matching author filter") ||
		strings.Contains(msg, "no valid commit dates by ")
}

// AuthorDateRangeMatch returns the earliest and latest commit dates across all
// matched author emails in gitDir.
//
// "No commits for this author" on an individual email is skipped so alternate
// addresses can still contribute. Context cancellation and operational git
// failures are returned immediately.
func AuthorDateRangeMatch(ctx context.Context, gitDir string, match AuthorMatch) (first, last time.Time, err error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, time.Time{}, err
	}

	queries := match.Emails
	if len(queries) == 0 && match.Filter != "" {
		queries = []string{match.Filter}
	}

	var found bool
	for _, q := range queries {
		if err := ctx.Err(); err != nil {
			return time.Time{}, time.Time{}, err
		}
		f, l, qErr := AuthorDateRange(ctx, gitDir, q)
		if qErr != nil {
			if ctx.Err() != nil {
				return time.Time{}, time.Time{}, ctx.Err()
			}
			if isNoAuthorCommits(qErr) {
				continue
			}
			// Operational failure (repo missing, git broken, etc.).
			return time.Time{}, time.Time{}, qErr
		}
		found = true
		if first.IsZero() || f.Before(first) {
			first = f
		}
		if last.IsZero() || l.After(last) {
			last = l
		}
	}
	if !found {
		return time.Time{}, time.Time{}, camperrors.Newf("no commits matching author filter %q in %s", match.Filter, gitDir)
	}
	return first, last, nil
}

// AuthorOwnershipFraction returns the fraction of blamed lines in proj owned by
// the matched author set. When blame data is missing, returns 1.0 so projects
// the author committed to are not zeroed out.
func AuthorOwnershipFraction(proj ResolvedProject, match AuthorMatch, resolver *AuthorResolver) float64 {
	if match.Filter == "" {
		return 1.0
	}
	if len(proj.Authors) == 0 {
		return 1.0
	}

	var authorLines, totalLines int
	for _, c := range proj.Authors {
		totalLines += c.Lines
		if match.MatchesAuthor(resolver, c.Email) {
			authorLines += c.Lines
		}
	}
	if totalLines == 0 {
		return 1.0
	}
	return float64(authorLines) / float64(totalLines)
}

// MergedAuthorSpans maps canonical author ID → commit date span merged across
// unique git directories. Built with one git log pass per git dir.
type MergedAuthorSpans map[string]*authorDateSpan

// CollectMergedAuthorSpans computes per-author date spans across projects with
// one gitDirAuthors call per unique GitDir (not per author × email).
func CollectMergedAuthorSpans(ctx context.Context, projects []ResolvedProject, resolver *AuthorResolver) (MergedAuthorSpans, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resolver == nil {
		resolver = NewAuthorResolver(nil)
	}

	uniqueGitDirs := make(map[string]struct{})
	for _, p := range projects {
		if p.GitDir != "" {
			uniqueGitDirs[p.GitDir] = struct{}{}
		}
	}

	merged := make(MergedAuthorSpans)
	for gitDir := range uniqueGitDirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		byID, err := gitDirAuthors(ctx, gitDir, resolver)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		for authorID, span := range byID {
			if span == nil {
				continue
			}
			existing, ok := merged[authorID]
			if !ok {
				cp := *span
				merged[authorID] = &cp
				continue
			}
			existing.merge(span)
		}
	}
	return merged, nil
}

// PersonMonths returns elapsed months for authorID, floored at minAuthorMonths.
// Missing authors return minAuthorMonths.
func (s MergedAuthorSpans) PersonMonths(authorID string) float64 {
	span, ok := s[authorID]
	if !ok || span == nil || span.earliest.IsZero() {
		return minAuthorMonths
	}
	months := ElapsedMonths(span.earliest, span.latest)
	if months < minAuthorMonths {
		return minAuthorMonths
	}
	return months
}

// AuthorActualPersonMonths computes personal actual effort as the union of the
// matched author's commit date spans across unique git directories.
//
// When match.AuthorIDs is non-empty and resolver is non-nil, spans are taken from
// CollectMergedAuthorSpans (one git pass per repo). Otherwise falls back to
// per-email AuthorDateRangeMatch for ad-hoc filters.
//
// Operational git errors and cancellation are returned. A true "no commits in
// any repo" outcome yields minAuthorMonths (not an error).
func AuthorActualPersonMonths(ctx context.Context, projects []ResolvedProject, match AuthorMatch, resolver *AuthorResolver) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if match.Filter == "" {
		return minAuthorMonths, nil
	}

	// Fast path: configured / resolved author IDs → single pass per git dir.
	if resolver != nil && len(match.AuthorIDs) > 0 {
		spans, err := CollectMergedAuthorSpans(ctx, projects, resolver)
		if err != nil {
			return 0, err
		}
		var merged authorDateSpan
		var any bool
		for id := range match.AuthorIDs {
			span, ok := spans[id]
			if !ok || span == nil || span.earliest.IsZero() {
				continue
			}
			any = true
			merged.merge(span)
		}
		if !any {
			return minAuthorMonths, nil
		}
		months := ElapsedMonths(merged.earliest, merged.latest)
		if months < minAuthorMonths {
			months = minAuthorMonths
		}
		return months, nil
	}

	// Ad-hoc filter: query unique git dirs with the expanded email/filter list.
	uniqueGitDirs := make(map[string]struct{})
	for _, p := range projects {
		if p.GitDir != "" {
			uniqueGitDirs[p.GitDir] = struct{}{}
		}
	}

	var merged authorDateSpan
	var any bool
	for gitDir := range uniqueGitDirs {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		first, last, err := AuthorDateRangeMatch(ctx, gitDir, match)
		if err != nil {
			if isNoAuthorCommits(err) {
				continue
			}
			return 0, err
		}
		any = true
		merged.merge(&authorDateSpan{earliest: first, latest: last})
	}

	if !any || merged.earliest.IsZero() {
		return minAuthorMonths, nil
	}

	months := ElapsedMonths(merged.earliest, merged.latest)
	if months < minAuthorMonths {
		months = minAuthorMonths
	}
	return months, nil
}

// ScaleScoreForAuthor multiplies estimated effort fields by ownership fraction.
// EstimatedMonths is left unchanged so EstimatedPeople * EstimatedMonths scales
// with ownership. ActualPersonMonths and FullLeverage are recomputed by the caller.
func ScaleScoreForAuthor(score *LeverageScore, ownership float64) {
	if score == nil {
		return
	}
	if ownership < 0 {
		ownership = 0
	}
	if ownership > 1 {
		ownership = 1
	}
	score.EstimatedPeople *= ownership
	score.EstimatedCost *= ownership
	if score.ActualPeople > 0 {
		score.SimpleLeverage = score.EstimatedPeople / score.ActualPeople
	}
	if score.ActualPersonMonths > 0 {
		estPM := score.EstimatedPeople * score.EstimatedMonths
		score.FullLeverage = estPM / score.ActualPersonMonths
	}
}
