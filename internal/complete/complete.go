// Package complete provides completion candidate generation for shell integration.
package complete

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/nav/index"
)

// RichCategoryGroup groups rich completion candidates by category.
type RichCategoryGroup struct {
	Category   string
	Candidates []index.CompletionCandidate
}

// Timeout is the maximum time to spend generating completions.
// 200ms allows room for cold-start index rebuilds while staying responsive.
const Timeout = 200 * time.Millisecond

// Generate returns completion candidates for the given args.
// It uses a timeout to ensure shell responsiveness.
func Generate(ctx context.Context, args []string) ([]string, error) {
	// Add timeout to prevent blocking shell
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, nil
	}

	topLevelNames := nav.TopLevelNavigationNames(cfg)

	// No args - complete category shortcuts
	if len(args) == 0 {
		return topLevelNames, nil
	}

	resolved := nav.ResolveConfiguredTarget(cfg, args)
	if resolved.Matched {
		if resolved.RelativePath != "" {
			return completeInRelativePath(ctx, campaignRoot, resolved.RelativePath, resolved.Query)
		}
		if resolved.Drill {
			return completeDrillInCategory(ctx, campaignRoot, resolved.Category, resolved.Query)
		}
		// Complete within category
		return completeInCategory(ctx, resolved.Category, resolved.Query)
	}

	// Not a shortcut - complete from all targets and shortcuts
	return completeAll(ctx, args[0], topLevelNames)
}

// GenerateRich returns completion candidates with fuzzy matching and path descriptions.
// Results are grouped by category.
func GenerateRich(ctx context.Context, args []string) ([]RichCategoryGroup, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, nil
	}

	resolved := nav.ResolveConfiguredTarget(cfg, args)
	query := ""
	if len(args) > 0 {
		query = args[0]
	}
	cat := nav.CategoryAll
	if resolved.Matched {
		if resolved.RelativePath != "" {
			candidates, err := completeInRelativePathRich(ctx, campaignRoot, resolved.RelativePath, resolved.Query)
			if err != nil {
				return nil, err
			}
			return []RichCategoryGroup{{
				Category:   strings.TrimRight(resolved.RelativePath, "/"),
				Candidates: candidates,
			}}, nil
		}
		if resolved.Drill {
			candidates, err := completeDrillInCategoryRich(ctx, campaignRoot, resolved.Category, resolved.Query)
			if err != nil {
				return nil, err
			}
			return []RichCategoryGroup{{
				Category:   string(resolved.Category),
				Candidates: candidates,
			}}, nil
		}
		cat = resolved.Category
		query = resolved.Query
	}

	// Worktrees use hierarchical completion: project dirs first, then project@branch
	if resolved.Matched && cat == nav.CategoryWorktrees {
		candidates, err := completeWorktreeRich(ctx, campaignRoot, query)
		if err != nil {
			return nil, err
		}
		return []RichCategoryGroup{{
			Category:   string(nav.CategoryWorktrees),
			Candidates: candidates,
		}}, nil
	}

	// Subdirectory completion for queries containing "/"
	if resolved.Matched && strings.Contains(query, "/") {
		subdirCandidates, err := CompleteSubdirectoryRich(ctx, campaignRoot, cat, query)
		if err == nil && len(subdirCandidates) > 0 {
			return []RichCategoryGroup{{
				Category:   string(cat),
				Candidates: subdirCandidates,
			}}, nil
		}
	}

	// Get or build index
	idx, err := index.GetOrBuild(ctx, campaignRoot, false)
	if err != nil {
		return nil, err
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get fuzzy completion candidates
	q := index.NewQuery(idx)
	candidates := q.FuzzyComplete(query, cat)

	// Group by category
	categoryMap := make(map[string][]index.CompletionCandidate)
	for _, c := range candidates {
		categoryMap[c.Category] = append(categoryMap[c.Category], c)
	}

	grouped := make([]RichCategoryGroup, 0, len(categoryMap))
	for catName, cands := range categoryMap {
		grouped = append(grouped, RichCategoryGroup{
			Category:   catName,
			Candidates: cands,
		})
	}

	sort.Slice(grouped, func(i, j int) bool {
		return grouped[i].Category < grouped[j].Category
	})

	return grouped, nil
}

// CategoryShortcuts returns category shortcut keys from campaign config.
// Returns nil if not in a campaign.
func CategoryShortcuts() []string {
	ctx := context.Background()
	cfg, _, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil
	}
	return nav.TopLevelNavigationNames(cfg)
}

// completeInCategory returns completion candidates within a specific category.
func completeInCategory(ctx context.Context, cat nav.Category, query string) ([]string, error) {
	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Worktrees use hierarchical completion: project dirs first, then project@branch
	if cat == nav.CategoryWorktrees {
		return CompleteWorktree(ctx, query)
	}

	// Special handling for festivals with path syntax
	if cat == nav.CategoryFestivals && strings.Contains(query, "/") {
		return CompleteFestival(ctx, query)
	}

	// Get campaign root
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		return nil, err
	}

	// Special handling for flow paths (contains "/" and first segment is a flow)
	// This enables syntax like: cgo de myflow/active/item
	if strings.Contains(query, "/") {
		if candidates, handled, err := CompleteFlowInCategory(ctx, cat, jumpResult.Path, query); handled {
			return candidates, err
		}
		// Generic subdirectory completion for any "/" query not handled by flows
		if candidates, err := CompleteSubdirectory(ctx, jumpResult.Path, cat, query); err == nil && len(candidates) > 0 {
			return candidates, nil
		}
	}

	// Get or build index
	idx, err := index.GetOrBuild(ctx, jumpResult.Path, false)
	if err != nil {
		return nil, err
	}

	// Check context again
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Use index query for completion
	q := index.NewQuery(idx)

	if query == "" {
		// No query - return all targets in category
		targets := q.ByCategory(cat)
		candidates := make([]string, len(targets))
		for i, t := range targets {
			candidates[i] = t.Name
		}
		return candidates, nil
	}

	// Has query - filter by prefix
	candidates := q.Complete(query, cat)
	return candidates, nil
}

// completeAll returns completion candidates from all categories plus shortcuts.
func completeAll(ctx context.Context, query string, topLevelNames []string) ([]string, error) {
	var candidates []string

	// Add matching top-level navigation names first. Normalize both sides
	// (lowercase + trim trailing slash) so queries like "design/" still match
	// the "design" top-level name; this mirrors how ResolveConfiguredTarget
	// normalizes tokens elsewhere.
	normalizedQuery := nav.NormalizeNavigationName(query)
	for _, name := range topLevelNames {
		if strings.HasPrefix(nav.NormalizeNavigationName(name), normalizedQuery) {
			candidates = append(candidates, name)
		}
	}

	// Check context
	if ctx.Err() != nil {
		return candidates, ctx.Err()
	}

	// Get campaign root
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		// Return just shortcuts if not in campaign
		return candidates, nil
	}

	// Get or build index
	idx, err := index.GetOrBuild(ctx, jumpResult.Path, false)
	if err != nil {
		return candidates, nil
	}

	// Check context again
	if ctx.Err() != nil {
		return candidates, ctx.Err()
	}

	// Complete from all categories
	q := index.NewQuery(idx)
	targetCandidates := q.CompleteAny(query, nav.CategoryAll)
	candidates = append(candidates, targetCandidates...)

	return candidates, nil
}

func completeInRelativePath(ctx context.Context, campaignRoot, relativePath, query string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	basePath := filepath.Join(campaignRoot, relativePath)
	if query == "" {
		return listPathCandidates(ctx, basePath, "")
	}

	if strings.Contains(query, "/") {
		return completeSubdirectoryInPath(ctx, basePath, query)
	}

	return listPathCandidates(ctx, basePath, query)
}

func completeInRelativePathRich(ctx context.Context, campaignRoot, relativePath, query string) ([]index.CompletionCandidate, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	basePath := filepath.Join(campaignRoot, relativePath)
	if query == "" {
		return listPathCandidatesRich(ctx, basePath, relativePath, "")
	}

	if strings.Contains(query, "/") {
		return completeSubdirectoryInPathRich(ctx, basePath, relativePath, query)
	}

	return listPathCandidatesRich(ctx, basePath, relativePath, query)
}

func completeDrillInCategory(ctx context.Context, campaignRoot string, cat nav.Category, query string) ([]string, error) {
	basePath := categoryAbsDir(campaignRoot, cat)
	if basePath == "" {
		return nil, nil
	}
	if query == "" {
		return listPathCandidates(ctx, basePath, "")
	}
	if strings.Contains(query, "/") {
		return CompleteSubdirectory(ctx, campaignRoot, cat, query)
	}
	return listPathCandidates(ctx, basePath, query)
}

func completeDrillInCategoryRich(ctx context.Context, campaignRoot string, cat nav.Category, query string) ([]index.CompletionCandidate, error) {
	basePath := categoryAbsDir(campaignRoot, cat)
	if basePath == "" {
		return nil, nil
	}
	relativePath := string(cat)
	if query == "" {
		return listPathCandidatesRich(ctx, basePath, relativePath, "")
	}
	if strings.Contains(query, "/") {
		return CompleteSubdirectoryRich(ctx, campaignRoot, cat, query)
	}
	return listPathCandidatesRich(ctx, basePath, relativePath, query)
}

func listPathCandidates(ctx context.Context, absPath, prefix string) ([]string, error) {
	entries, err := readDirForCompletion(absPath)
	if err != nil {
		return nil, err
	}

	var candidates []string
	prefixLower := strings.ToLower(prefix)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return candidates, ctx.Err()
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if prefixLower != "" && !strings.HasPrefix(strings.ToLower(entry.Name()), prefixLower) {
			continue
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		candidates = append(candidates, name)
	}
	return candidates, nil
}

func listPathCandidatesRich(ctx context.Context, absPath, relativePath, prefix string) ([]index.CompletionCandidate, error) {
	entries, err := readDirForCompletion(absPath)
	if err != nil {
		return nil, err
	}

	var candidates []index.CompletionCandidate
	prefixLower := strings.ToLower(prefix)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return candidates, ctx.Err()
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if prefixLower != "" && !strings.HasPrefix(strings.ToLower(entry.Name()), prefixLower) {
			continue
		}

		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}

		candidates = append(candidates, index.CompletionCandidate{
			Name:     name,
			Path:     filepath.Join(relativePath, entry.Name()),
			Category: strings.TrimRight(relativePath, "/"),
		})
	}
	return candidates, nil
}

// readDirForCompletion reads a directory for completion purposes.
// A missing directory is not an error (returns nil entries), but real I/O
// failures — permission denied, bad symlinks, etc. — are surfaced so callers
// can decide how to handle them instead of silently degrading to "no matches".
func readDirForCompletion(absPath string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

func completeSubdirectoryInPath(ctx context.Context, basePath, query string) ([]string, error) {
	lastSlash := strings.LastIndex(query, "/")
	dirPath := query[:lastSlash]
	filter := query[lastSlash+1:]

	absDir := filepath.Join(basePath, dirPath)
	entries, err := readDirForCompletion(absDir)
	if err != nil {
		return nil, err
	}

	var candidates []string
	filterLower := strings.ToLower(filter)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return candidates, ctx.Err()
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if filterLower != "" && !strings.HasPrefix(strings.ToLower(entry.Name()), filterLower) {
			continue
		}
		name := dirPath + "/" + entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		candidates = append(candidates, name)
	}
	return candidates, nil
}

func completeSubdirectoryInPathRich(ctx context.Context, basePath, relativePath, query string) ([]index.CompletionCandidate, error) {
	lastSlash := strings.LastIndex(query, "/")
	dirPath := query[:lastSlash]
	filter := query[lastSlash+1:]

	absDir := filepath.Join(basePath, dirPath)
	entries, err := readDirForCompletion(absDir)
	if err != nil {
		return nil, err
	}

	var candidates []index.CompletionCandidate
	filterLower := strings.ToLower(filter)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return candidates, ctx.Err()
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if filterLower != "" && !strings.HasPrefix(strings.ToLower(entry.Name()), filterLower) {
			continue
		}

		name := dirPath + "/" + entry.Name()
		if entry.IsDir() {
			name += "/"
		}

		candidates = append(candidates, index.CompletionCandidate{
			Name:     name,
			Path:     filepath.Join(relativePath, dirPath, entry.Name()),
			Category: strings.TrimRight(relativePath, "/"),
		})
	}
	return candidates, nil
}
