package commit

import (
	"path/filepath"
	"strings"
)

// NormalizeFiles converts mixed absolute/relative paths to repo-relative files
// suitable for selective staging, dropping duplicates and unsafe entries.
func NormalizeFiles(campaignRoot string, files ...string) []string {
	if len(files) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(files))
	normalized := make([]string, 0, len(files))

	for _, file := range files {
		if file == "" {
			continue
		}

		p := filepath.Clean(file)
		if p == "." {
			continue
		}

		if filepath.IsAbs(p) && campaignRoot != "" {
			rel, err := filepath.Rel(campaignRoot, p)
			if err != nil {
				continue
			}
			rel = filepath.Clean(rel)
			if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			p = rel
		}

		if p == "." || p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
			continue
		}

		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		normalized = append(normalized, p)
	}

	return normalized
}
