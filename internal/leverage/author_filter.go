package leverage

import (
	"context"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// AuthorMatch is the result of expanding an --author filter against authors.json
// and free-form email/name substrings.
type AuthorMatch struct {
	// Filter is the original user-provided filter string.
	Filter string

	// AuthorIDs are canonical resolver IDs matched by the filter.
	// Empty when the filter only matches as a raw git --author substring
	// (no authors.json group).
	AuthorIDs map[string]bool

	// Emails are concrete addresses to query with git log --author=
	// (group emails plus the raw filter when useful).
	Emails []string
}

// ExpandAuthorFilter resolves an --author flag against the author config.
//
// Matching is case-insensitive substring against author ID, display name, and
// configured emails. When a configured identity matches, all of its emails are
// included so personal mode does not miss alternate commit addresses.
//
// When nothing in the config matches, the raw filter is still used as a single
// git author substring (preserving prior behavior for ad-hoc emails).
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
			matched := strings.Contains(strings.ToLower(id), lower) ||
				strings.Contains(strings.ToLower(identity.DisplayName), lower)
			if !matched {
				for _, email := range identity.Emails {
					if strings.Contains(strings.ToLower(email), lower) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
			m.AuthorIDs[id] = true
			for _, email := range identity.Emails {
				addEmail(email)
			}
		}
	}

	// Always keep the raw filter for git substring match (covers unlisted emails
	// and partial strings like "lancekrogers").
	addEmail(filter)

	// If we only matched via raw filter, seed AuthorIDs from resolver so ownership
	// lookups work when the filter is an exact configured email. Skip excluded
	// identities (bots/test accounts) so they do not become "personal" targets.
	if len(m.AuthorIDs) == 0 && resolver != nil && !resolver.IsExcluded(filter) {
		id := resolver.Resolve(filter)
		// Resolve falls back to lowercase email for unknowns — still useful as a key.
		if id != "" {
			m.AuthorIDs[id] = true
		}
	}

	return m
}

// MatchesAuthor reports whether email belongs to the matched personal filter set.
func (m AuthorMatch) MatchesAuthor(resolver *AuthorResolver, email string) bool {
	if m.Filter == "" {
		return false
	}
	if resolver != nil && len(m.AuthorIDs) > 0 {
		if m.AuthorIDs[resolver.Resolve(email)] {
			return true
		}
	}
	lowerEmail := strings.ToLower(strings.TrimSpace(email))
	lowerFilter := strings.ToLower(m.Filter)
	if strings.Contains(lowerEmail, lowerFilter) {
		return true
	}
	for _, e := range m.Emails {
		if strings.EqualFold(e, email) || strings.Contains(lowerEmail, strings.ToLower(e)) {
			return true
		}
	}
	return false
}

// AuthorHasCommitsMatch reports whether any matched identity has commits in gitDir.
func AuthorHasCommitsMatch(ctx context.Context, gitDir string, match AuthorMatch) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// Prefer distinct email queries so alternate addresses are found.
	// Fall back to raw filter substring (single git call) when emails empty.
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

// AuthorDateRangeMatch returns the earliest and latest commit dates across all
// matched author emails in gitDir.
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
		f, l, qErr := AuthorDateRange(ctx, gitDir, q)
		if qErr != nil {
			continue
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

// AuthorActualPersonMonths computes personal actual effort as the union of the
// matched author's commit date spans across unique git directories.
//
// This intentionally does NOT sum per-project spans (which multi-counts parallel
// multi-repo work and monorepo subprojects that share a GitDir).
func AuthorActualPersonMonths(ctx context.Context, projects []ResolvedProject, match AuthorMatch) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if match.Filter == "" {
		return minAuthorMonths, nil
	}

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
			continue
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
	// Keep TotalCode/TotalLines as full-tree inventory for transparency, but scale
	// is applied on cost/people which drive leverage.
	if score.ActualPeople > 0 {
		score.SimpleLeverage = score.EstimatedPeople / score.ActualPeople
	}
	if score.ActualPersonMonths > 0 {
		estPM := score.EstimatedPeople * score.EstimatedMonths
		score.FullLeverage = estPM / score.ActualPersonMonths
	}
}
