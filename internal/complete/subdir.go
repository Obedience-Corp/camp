package complete

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/nav/index"
)

// CompleteSubdirectory returns filesystem-based subdirectory completion candidates
// for a query containing "/". It resolves the first segment against the navigation
// index, then lists entries from the filesystem at the resolved subpath.
//
// For example, with cat=CategoryDesign and query="festival_app/src":
//   - Resolves "festival_app" in workflow/design/
//   - Lists entries in workflow/design/festival_app/ matching prefix "src"
//   - Returns entries like "festival_app/src/" (directories) or "festival_app/src/file.go" (files)
func CompleteSubdirectory(ctx context.Context, campaignRoot string, cat nav.Category, query string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if !strings.Contains(query, "/") {
		return nil, nil
	}

	// Split query into directory prefix and filter suffix
	// e.g. "festival_app/src/comp" -> dirPath="festival_app/src", filter="comp"
	lastSlash := strings.LastIndex(query, "/")
	dirPath := query[:lastSlash]
	filter := query[lastSlash+1:]

	// Build the absolute path to scan
	catDir := categoryAbsDir(campaignRoot, cat)
	if catDir == "" {
		return nil, nil
	}

	absDir := filepath.Join(catDir, dirPath)

	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, nil // directory doesn't exist — no completions
	}

	var candidates []string
	filterLower := strings.ToLower(filter)

	for _, entry := range entries {
		if ctx.Err() != nil {
			return candidates, ctx.Err()
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if filterLower != "" && !strings.HasPrefix(strings.ToLower(name), filterLower) {
			continue
		}
		if entry.IsDir() {
			candidates = append(candidates, dirPath+"/"+name+"/")
		} else {
			candidates = append(candidates, dirPath+"/"+name)
		}
	}

	return candidates, nil
}

// CompleteSubdirectoryRich returns rich completion candidates for subdirectory paths.
// Same logic as CompleteSubdirectory but returns []CompletionCandidate with metadata.
func CompleteSubdirectoryRich(ctx context.Context, campaignRoot string, cat nav.Category, query string) ([]index.CompletionCandidate, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if !strings.Contains(query, "/") {
		return nil, nil
	}

	lastSlash := strings.LastIndex(query, "/")
	dirPath := query[:lastSlash]
	filter := query[lastSlash+1:]

	catDir := categoryAbsDir(campaignRoot, cat)
	if catDir == "" {
		return nil, nil
	}

	absDir := filepath.Join(catDir, dirPath)

	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, nil
	}

	var candidates []index.CompletionCandidate
	filterLower := strings.ToLower(filter)

	for _, entry := range entries {
		if ctx.Err() != nil {
			return candidates, ctx.Err()
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if filterLower != "" && !strings.HasPrefix(strings.ToLower(name), filterLower) {
			continue
		}

		completionName := dirPath + "/" + name
		if entry.IsDir() {
			completionName += "/"
		}

		relPath := filepath.Join(string(cat), dirPath, name)
		candidates = append(candidates, index.CompletionCandidate{
			Name:     completionName,
			Path:     relPath,
			Category: string(cat),
		})
	}

	return candidates, nil
}

// categoryAbsDir returns the absolute directory path for a category within a campaign root.
// Returns empty string if the category has no directory mapping.
func categoryAbsDir(campaignRoot string, cat nav.Category) string {
	if cat == nav.CategoryAll || cat == "" {
		return campaignRoot
	}
	return filepath.Join(campaignRoot, cat.Dir())
}
