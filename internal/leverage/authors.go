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
func AuthorDateRange(ctx context.Context, gitDir, authorEmail string) (first, last time.Time, err error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, time.Time{}, err
	}

	// Earliest commit by author
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "log", "--all",
		"--author="+authorEmail, "--reverse", "--format=%cI", "-1")
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("git log (first): %w", err)
	}
	firstStr := strings.TrimSpace(string(out))
	if firstStr == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("no commits by %s in %s", authorEmail, gitDir)
	}
	first, err = time.Parse(time.RFC3339, firstStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parsing first commit date: %w", err)
	}

	// Latest commit by author
	cmd = exec.CommandContext(ctx, "git", "-C", gitDir, "log", "--all",
		"--author="+authorEmail, "--format=%cI", "-1")
	out, err = cmd.Output()
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("git log (last): %w", err)
	}
	lastStr := strings.TrimSpace(string(out))
	if lastStr == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("no commits by %s in %s", authorEmail, gitDir)
	}
	last, err = time.Parse(time.RFC3339, lastStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parsing last commit date: %w", err)
	}

	return first, last, nil
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
