package leverage

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// allAuthorDates returns earliest/latest commit dates per email in one git call.
func allAuthorDates(ctx context.Context, gitDir string) (map[string]*authorDateSpan, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "log", "--all", "--format=%ae\t%cI")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	result := make(map[string]*authorDateSpan)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 2)
		if len(parts) != 2 {
			continue
		}
		email, dateStr := parts[0], parts[1]
		t, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			continue
		}
		span, ok := result[email]
		if !ok {
			span = &authorDateSpan{}
			result[email] = span
		}
		if span.earliest.IsZero() || t.Before(span.earliest) {
			span.earliest = t
		}
		if span.latest.IsZero() || t.After(span.latest) {
			span.latest = t
		}
	}
	return result, nil
}

// authorAccum tracks per-author line counts during blame parsing.
type authorAccum struct {
	name  string
	email string
	lines int
}

// GetAuthorLOC computes per-author LOC ownership for a directory using git blame.
// It runs git blame --line-porcelain on each tracked source file, counts lines
// per author email, and returns AuthorContribution slices sorted by lines descending.
func GetAuthorLOC(ctx context.Context, dir string) ([]AuthorContribution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	files, err := trackedFiles(ctx, dir)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, nil
	}

	accum := make(map[string]*authorAccum)

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if err := blameFile(ctx, dir, file, accum); err != nil {
			// Skip files that can't be blamed (binary files, etc.)
			continue
		}
	}

	return buildContributions(accum), nil
}

// trackedFiles returns all git-tracked files under dir.
func trackedFiles(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "ls-files")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// blameFile runs git blame --line-porcelain on a single file and accumulates
// line counts per author into the accum map (keyed by email).
func blameFile(ctx context.Context, dir, file string, accum map[string]*authorAccum) error {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "blame", "--line-porcelain", file)
	out, err := cmd.Output()
	if err != nil {
		return err // binary files, empty files, etc.
	}

	var curName, curEmail string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "author "):
			curName = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-mail "):
			// Strip angle brackets: <email@example.com> -> email@example.com
			mail := strings.TrimPrefix(line, "author-mail ")
			mail = strings.TrimPrefix(mail, "<")
			mail = strings.TrimSuffix(mail, ">")
			curEmail = mail
		case strings.HasPrefix(line, "\t"):
			// Content line = one attributed line
			if curEmail != "" {
				a, ok := accum[curEmail]
				if !ok {
					a = &authorAccum{name: curName, email: curEmail}
					accum[curEmail] = a
				}
				a.lines++
			}
		}
	}

	return nil
}

// CountAuthors returns the number of distinct human authors for a git repo.
// Uses git shortlog for speed (no blame needed). Filters bot accounts and
// deduplicates email aliases (same name = 1 person). Returns minimum 1.
func CountAuthors(ctx context.Context, gitDir string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "shortlog", "-sne", "--all")
	out, err := cmd.Output()
	if err != nil {
		return 1, nil // fallback: assume 1 author
	}

	// Parse "  123\tName <email>" lines
	names := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Format: "  N\tName <email>"
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		nameEmail := strings.TrimSpace(parts[1])
		name, email := parseNameEmail(nameEmail)

		if isBotEmail(email) {
			continue
		}

		// Deduplicate by normalized name
		normName := strings.ToLower(strings.TrimSpace(name))
		if normName != "" {
			names[normName] = true
		}
	}

	if len(names) == 0 {
		return 1, nil
	}
	return len(names), nil
}

// AuthorHasCommits returns true if the given author email has commits in the repo.
func AuthorHasCommits(ctx context.Context, gitDir, authorEmail string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "log", "--all",
		"--author="+authorEmail, "--oneline", "-1")
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// AuthorDateRange returns the first and last commit dates for a specific author.
// Gets all commit dates in one call and extracts the earliest and latest.
func AuthorDateRange(ctx context.Context, gitDir, authorEmail string) (first, last time.Time, err error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, time.Time{}, err
	}

	// Get all commit dates for this author in one call.
	// Note: --reverse with -1 does NOT work (git applies --max-count before --reverse).
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "log", "--all",
		"--author="+authorEmail, "--format=%cI")
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("git log: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("no commits by %s in %s", authorEmail, gitDir)
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		t, parseErr := time.Parse(time.RFC3339, line)
		if parseErr != nil {
			continue
		}
		if first.IsZero() || t.Before(first) {
			first = t
		}
		if last.IsZero() || t.After(last) {
			last = t
		}
	}

	if first.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("no valid commit dates by %s in %s", authorEmail, gitDir)
	}

	return first, last, nil
}

// authorDateSpan holds the earliest and latest commit dates for a single author.
type authorDateSpan struct {
	earliest time.Time
	latest   time.Time
}

// gitDirAuthors enumerates all non-bot authors in a git repo and computes their
// date ranges. Returns a map keyed by normalized name.
//
// Uses allAuthorDates for a single git-log pass instead of spawning one git
// process per email, reducing total git processes from 1+N to 2.
func gitDirAuthors(ctx context.Context, gitDir string) (map[string]*authorDateSpan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "shortlog", "-sne", "--all")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git shortlog: %w", err)
	}

	// Parse shortlog and group emails by normalized name.
	type authorInfo struct {
		emails []string
	}
	byName := make(map[string]*authorInfo)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name, email := parseNameEmail(strings.TrimSpace(parts[1]))
		if isBotEmail(email) {
			continue
		}
		normName := strings.ToLower(strings.TrimSpace(name))
		if normName == "" {
			continue
		}
		a, ok := byName[normName]
		if !ok {
			a = &authorInfo{}
			byName[normName] = a
		}
		a.emails = append(a.emails, email)
	}

	// Fetch all author dates in a single git-log pass.
	allDates, err := allAuthorDates(ctx, gitDir)
	if err != nil {
		return nil, fmt.Errorf("fetching author dates: %w", err)
	}

	// For each person, find earliest/latest across all their emails.
	result := make(map[string]*authorDateSpan, len(byName))
	for normName, info := range byName {
		span := &authorDateSpan{}
		for _, email := range info.emails {
			emailSpan, ok := allDates[email]
			if !ok {
				continue
			}
			if span.earliest.IsZero() || emailSpan.earliest.Before(span.earliest) {
				span.earliest = emailSpan.earliest
			}
			if span.latest.IsZero() || emailSpan.latest.After(span.latest) {
				span.latest = emailSpan.latest
			}
		}
		result[normName] = span
	}

	return result, nil
}

// BlameWeightedPersonMonths computes blame-weighted person-months for a project.
// Each author's PM is their date span weighted by their LOC ownership share,
// producing accurate effort when low-contribution authors have wide date spans.
//
// Authors with < 1% LOC ownership are excluded from the effort calculation (D2).
// Returns total weighted PM and enriched AuthorContribution slice with WeightedPM set.
func BlameWeightedPersonMonths(ctx context.Context, gitDir, sccDir string) (float64, []AuthorContribution, error) {
	const minAuthorMonths = 0.1
	const minPercentage = 1.0

	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}

	// Get date spans per author (keyed by normalized name).
	dateSpans, err := gitDirAuthors(ctx, gitDir)
	if err != nil {
		return minAuthorMonths, nil, nil
	}

	// Get blame LOC per author.
	contribs, err := GetAuthorLOC(ctx, sccDir)
	if err != nil || len(contribs) == 0 {
		// Fall back to unweighted date-span PM.
		return dateSpanOnlyPM(dateSpans, minAuthorMonths), nil, nil
	}

	if len(dateSpans) == 0 {
		return minAuthorMonths, nil, nil
	}

	var totalPM float64
	enriched := make([]AuthorContribution, 0, len(contribs))

	for _, c := range contribs {
		if c.Percentage < minPercentage {
			continue
		}

		normName := strings.ToLower(strings.TrimSpace(c.Name))
		span, ok := dateSpans[normName]
		if !ok {
			// Author in blame but not matched in commit log by name.
			// Use minimum PM weighted by their LOC share.
			c.WeightedPM = minAuthorMonths * (c.Percentage / 100.0)
			totalPM += c.WeightedPM
			enriched = append(enriched, c)
			continue
		}

		months := ElapsedMonths(span.earliest, span.latest)
		if months < minAuthorMonths {
			months = minAuthorMonths
		}

		c.WeightedPM = months * (c.Percentage / 100.0)
		totalPM += c.WeightedPM
		enriched = append(enriched, c)
	}

	if totalPM < minAuthorMonths {
		totalPM = minAuthorMonths
	}

	return totalPM, enriched, nil
}

// ProjectActualPersonMonths computes blame-weighted actual person-months for a
// project. Each author's date span is weighted by their LOC ownership share,
// producing accurate effort when low-contribution authors have wide date spans.
//
// When sccDir is non-empty, uses blame-weighted calculation via
// BlameWeightedPersonMonths. When sccDir is empty, falls back to unweighted
// date-span calculation for backward compatibility.
func ProjectActualPersonMonths(ctx context.Context, gitDir, sccDir string) (float64, error) {
	if sccDir != "" {
		pm, _, err := BlameWeightedPersonMonths(ctx, gitDir, sccDir)
		return pm, err
	}

	// Fallback: unweighted date-span PM (no blame data available).
	const minAuthorMonths = 0.1

	if err := ctx.Err(); err != nil {
		return 0, err
	}

	authors, err := gitDirAuthors(ctx, gitDir)
	if err != nil {
		return minAuthorMonths, nil
	}

	return dateSpanOnlyPM(authors, minAuthorMonths), nil
}

// parseNameEmail extracts name and email from "Name <email>" format.
func parseNameEmail(s string) (name, email string) {
	idx := strings.LastIndex(s, " <")
	if idx == -1 {
		return s, ""
	}
	name = s[:idx]
	email = strings.TrimSuffix(s[idx+2:], ">")
	return name, email
}

// isBotEmail returns true if the email looks like a bot account.
func isBotEmail(email string) bool {
	lower := strings.ToLower(email)
	botPatterns := []string{"noreply", "bot@", "[bot]", "github-actions", "dependabot", "renovate"}
	for _, pattern := range botPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// buildContributions converts the accumulator map into sorted AuthorContribution slices.
func buildContributions(accum map[string]*authorAccum) []AuthorContribution {
	if len(accum) == 0 {
		return nil
	}

	var total int
	for _, a := range accum {
		total += a.lines
	}

	if total == 0 {
		return nil
	}

	result := make([]AuthorContribution, 0, len(accum))
	for _, a := range accum {
		result = append(result, AuthorContribution{
			Name:       a.name,
			Email:      a.email,
			Lines:      a.lines,
			Percentage: float64(a.lines) / float64(total) * 100,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Lines > result[j].Lines
	})

	return result
}
