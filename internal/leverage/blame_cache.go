package leverage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BlameCacheEntry stores cached blame data for a single project.
type BlameCacheEntry struct {
	Project    string                    `json:"project"`
	CommitHash string                    `json:"commit_hash"`
	SCCDir     string                    `json:"scc_dir"`
	CachedAt   time.Time                 `json:"cached_at"`
	FileBlame  map[string]map[string]int `json:"file_blame"`

	// EmailToName maps author email → display name from git blame output.
	// Used by RecomputeAggregates to resolve real author names from FileBlame
	// (which only stores emails as keys). Without this mapping, the name-based
	// join with git date spans fails and actual PM collapses to the 0.1 floor.
	EmailToName map[string]string `json:"email_to_name,omitempty"`

	AuthorCount int                  `json:"author_count"`
	ActualPM    float64              `json:"actual_person_months"`
	Authors     []AuthorContribution `json:"authors"`
}

// BlameCache provides three-tier caching for git blame data.
// Each project gets a separate JSON file under the cache directory.
type BlameCache struct {
	dir string
}

// NewBlameCache returns a cache backed by the given directory.
func NewBlameCache(dir string) *BlameCache {
	return &BlameCache{dir: dir}
}

// DefaultCacheDir returns the standard cache location within a campaign.
func DefaultCacheDir(campaignRoot string) string {
	return filepath.Join(campaignRoot, ".campaign", "leverage", "cache")
}

// cacheFile returns the path for a project's cache entry.
func (c *BlameCache) cacheFile(project string) string {
	return filepath.Join(c.dir, project+".json")
}

// Load reads the cached blame entry for project. Returns nil, nil when the
// cache file is missing or corrupt (treat as cold cache).
func (c *BlameCache) Load(ctx context.Context, project string) (*BlameCacheEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(c.cacheFile(project))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, nil // corrupt or unreadable → cold cache
	}

	var entry BlameCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, nil // corrupt JSON → cold cache
	}

	return &entry, nil
}

// Save persists a cache entry to disk, creating directories as needed.
func (c *BlameCache) Save(ctx context.Context, entry *BlameCacheEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling cache entry: %w", err)
	}

	path := c.cacheFile(entry.Project)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing cache file: %w", err)
	}

	return nil
}

// RelPath computes the relative path from base to target. Exported for use
// by the CLI layer when computing monorepo subpaths.
func RelPath(base, target string) (string, error) {
	return filepath.Rel(base, target)
}

// ProjectHash returns a hash identifying the current state of a project's code.
// For standalone repos this is HEAD commit hash; for monorepo subprojects this
// is the tree hash at the subpath within HEAD.
func ProjectHash(ctx context.Context, p *ResolvedProject) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if p.InMonorepo {
		// Tree hash for the subpath within the monorepo.
		relPath, err := filepath.Rel(p.GitDir, p.SCCDir)
		if err != nil {
			return "", fmt.Errorf("computing relative path: %w", err)
		}
		ref := "HEAD:" + relPath
		cmd := exec.CommandContext(ctx, "git", "-C", p.GitDir, "rev-parse", ref)
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("git rev-parse %s: %w", ref, err)
		}
		return strings.TrimSpace(string(out)), nil
	}

	cmd := exec.CommandContext(ctx, "git", "-C", p.GitDir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ChangedFiles returns files modified, added, and deleted between two commits.
// When subpath is non-empty, results are scoped to that subdirectory.
func ChangedFiles(ctx context.Context, gitDir, oldHash, newHash, subpath string) (modified, added, deleted []string, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}

	args := []string{"-C", gitDir, "diff", "--name-status", oldHash, newHash}
	if subpath != "" {
		args = append(args, "--", subpath)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("git diff --name-status: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		status, file := parts[0], parts[1]

		// For monorepo subpaths, make file relative to the subpath.
		if subpath != "" {
			rel, relErr := filepath.Rel(subpath, file)
			if relErr == nil {
				file = rel
			}
		}

		switch {
		case strings.HasPrefix(status, "M"):
			modified = append(modified, file)
		case strings.HasPrefix(status, "A"):
			added = append(added, file)
		case strings.HasPrefix(status, "D"):
			deleted = append(deleted, file)
		case strings.HasPrefix(status, "R"):
			// Rename: old\tnew — the file field contains "old\tnew"
			renameParts := strings.SplitN(file, "\t", 2)
			if len(renameParts) == 2 {
				deleted = append(deleted, renameParts[0])
				added = append(added, renameParts[1])
			}
		}
	}

	return modified, added, deleted, nil
}

// IncrementalUpdate re-blames only the modified and added files, removes
// deleted files from FileBlame, and updates the entry in place.
func (entry *BlameCacheEntry) IncrementalUpdate(ctx context.Context, dir string, modified, added, deleted []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Remove deleted files.
	for _, f := range deleted {
		delete(entry.FileBlame, f)
	}

	// Re-blame modified and added files.
	toBlame := make([]string, 0, len(modified)+len(added))
	toBlame = append(toBlame, modified...)
	toBlame = append(toBlame, added...)

	for _, f := range toBlame {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		accum := make(map[string]*authorAccum)
		if err := blameFile(ctx, dir, f, accum); err != nil {
			// File might be binary or unblamed; remove stale entry.
			delete(entry.FileBlame, f)
			continue
		}

		perFile := make(map[string]int, len(accum))
		for email, a := range accum {
			perFile[email] = a.lines
		}
		entry.FileBlame[f] = perFile
	}

	return nil
}

// RecomputeAggregates rebuilds the Authors slice and AuthorCount from FileBlame.
func (entry *BlameCacheEntry) RecomputeAggregates() {
	if len(entry.FileBlame) == 0 {
		entry.Authors = nil
		entry.AuthorCount = 0
		return
	}

	// Aggregate lines per email across all files.
	accum := make(map[string]*authorAccum)
	for _, perFile := range entry.FileBlame {
		for email, lines := range perFile {
			if a, ok := accum[email]; ok {
				a.lines += lines
			} else {
				// Resolve real author name from cached mapping.
				// Falls back to email for old caches without EmailToName.
				name := email
				if entry.EmailToName != nil {
					if n, ok := entry.EmailToName[email]; ok {
						name = n
					}
				}
				accum[email] = &authorAccum{
					name:  name,
					email: email,
					lines: lines,
				}
			}
		}
	}

	entry.Authors = buildContributions(accum)
	entry.AuthorCount = len(entry.Authors)
}

// RecomputeProjectMetrics refreshes a ResolvedProject's metrics from a
// cache entry without re-running the full blame pipeline. Gets fresh date
// spans from git (fast) and combines with cached blame data.
func RecomputeProjectMetrics(ctx context.Context, p *ResolvedProject, entry *BlameCacheEntry) {
	if err := ctx.Err(); err != nil {
		return
	}

	count, err := CountAuthors(ctx, p.GitDir)
	if err == nil {
		p.AuthorCount = count
	}

	dateSpans, err := gitDirAuthors(ctx, p.GitDir)
	if err != nil || len(entry.Authors) == 0 {
		p.ActualPersonMonths = entry.ActualPM
		p.Authors = entry.Authors
		return
	}

	totalPM, enriched := blameWeightedPMFromContribs(entry.Authors, dateSpans)
	p.ActualPersonMonths = totalPM
	p.Authors = enriched

	// Update the cache entry with recomputed values.
	entry.ActualPM = totalPM
	entry.Authors = enriched
	entry.AuthorCount = p.AuthorCount
}

// PopulateOneProjectCached performs a full blame computation, captures per-file
// data for caching, and saves the result. Populates the ResolvedProject fields.
func PopulateOneProjectCached(ctx context.Context, p *ResolvedProject, cache *BlameCache, hash string) {
	if err := ctx.Err(); err != nil {
		return
	}

	count, err := CountAuthors(ctx, p.GitDir)
	if err == nil {
		p.AuthorCount = count
	}

	contribs, fileBlame, err := AuthorLOCWithFiles(ctx, p.SCCDir)
	if err != nil || len(contribs) == 0 {
		// Fall back to non-cached path.
		PopulateOneProject(ctx, p)
		return
	}

	dateSpans, err := gitDirAuthors(ctx, p.GitDir)
	if err != nil {
		p.Authors = contribs
		return
	}

	totalPM, enriched := blameWeightedPMFromContribs(contribs, dateSpans)
	p.ActualPersonMonths = totalPM
	p.Authors = enriched

	// Build email→name mapping from blame contributions for cache persistence.
	emailToName := make(map[string]string, len(contribs))
	for _, c := range contribs {
		if c.Email != "" && c.Name != "" {
			emailToName[c.Email] = c.Name
		}
	}

	// Build and save cache entry.
	entry := &BlameCacheEntry{
		Project:     p.Name,
		CommitHash:  hash,
		SCCDir:      p.SCCDir,
		CachedAt:    time.Now(),
		FileBlame:   fileBlame,
		EmailToName: emailToName,
		AuthorCount: p.AuthorCount,
		ActualPM:    totalPM,
		Authors:     enriched,
	}

	// Best-effort save; don't fail the operation on cache write error.
	_ = cache.Save(ctx, entry)
}
