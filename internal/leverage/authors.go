package leverage

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
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
