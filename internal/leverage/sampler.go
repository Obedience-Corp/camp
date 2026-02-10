package leverage

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// CommitSample represents a single commit selected for backfill processing.
type CommitSample struct {
	Hash      string    // Full commit hash
	Date      time.Time // Commit date (committer date)
	WeekStart time.Time // Monday of the ISO week this commit belongs to
}

// weekMonday returns the Monday of the ISO week containing t.
func weekMonday(t time.Time) time.Time {
	year, week := t.ISOWeek()
	// Find Jan 4 of the ISO year (always in ISO week 1)
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, t.Location())
	weekday := jan4.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	monday1 := jan4.AddDate(0, 0, -int(weekday-time.Monday))
	return monday1.AddDate(0, 0, (week-1)*7)
}

// SampleWeeklyCommits returns one commit per ISO week from the git history of gitDir.
// Commits are returned in chronological order. The first and latest commits are always included.
// The since parameter limits how far back to sample; zero value means no limit.
func SampleWeeklyCommits(ctx context.Context, gitDir string, since time.Time) ([]CommitSample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	args := []string{"-C", gitDir, "log", "--all", "--format=%H %cI", "--reverse"}
	if !since.IsZero() {
		args = append(args, "--since="+since.Format(time.RFC3339))
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil, nil
	}

	// Parse all commits
	type commitEntry struct {
		hash string
		date time.Time
	}
	var commits []commitEntry
	for _, line := range lines {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		date, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			continue
		}
		commits = append(commits, commitEntry{hash: parts[0], date: date})
	}

	if len(commits) == 0 {
		return nil, nil
	}

	// Group by ISO week — last commit per week wins
	type weekKey struct {
		year int
		week int
	}
	weekMap := make(map[weekKey]commitEntry)
	for _, c := range commits {
		y, w := c.date.ISOWeek()
		key := weekKey{year: y, week: w}
		weekMap[key] = c // overwrites — last commit per week
	}

	// Convert to sorted slice
	var samples []CommitSample
	for _, c := range weekMap {
		samples = append(samples, CommitSample{
			Hash:      c.hash,
			Date:      c.date,
			WeekStart: weekMonday(c.date),
		})
	}
	sort.Slice(samples, func(i, j int) bool {
		return samples[i].Date.Before(samples[j].Date)
	})

	// Ensure first commit is included
	first := commits[0]
	if samples[0].Hash != first.hash {
		firstSample := CommitSample{
			Hash:      first.hash,
			Date:      first.date,
			WeekStart: weekMonday(first.date),
		}
		samples = append([]CommitSample{firstSample}, samples...)
	}

	// Ensure latest commit is included
	latest := commits[len(commits)-1]
	if samples[len(samples)-1].Hash != latest.hash {
		latestSample := CommitSample{
			Hash:      latest.hash,
			Date:      latest.date,
			WeekStart: weekMonday(latest.date),
		}
		samples = append(samples, latestSample)
	}

	return samples, nil
}
