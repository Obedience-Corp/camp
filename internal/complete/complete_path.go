package complete

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/nav/index"
)

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
	relativePath := cat.Dir()
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
	entries = orderPathCandidates(absPath, entries)

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
	entries = orderPathCandidates(absPath, entries)

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
// failures - permission denied, bad symlinks, etc. - are surfaced so callers
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

func orderPathCandidates(absPath string, entries []os.DirEntry) []os.DirEntry {
	if len(entries) == 0 {
		return entries
	}
	names := make([]string, len(entries))
	indexByName := make(map[string]int, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
		indexByName[entry.Name()] = i
	}
	ordered := applyRecentFirstOrder(absPath, names)
	out := make([]os.DirEntry, len(entries))
	for i, n := range ordered {
		out[i] = entries[indexByName[n]]
	}
	return out
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
