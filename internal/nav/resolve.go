package nav

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav/fuzzy"
)

// ResolveRelativePathNavigation resolves a configured relative path plus optional query.
func ResolveRelativePathNavigation(ctx context.Context, campaignRoot, relativePath, query string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if query == "" {
		jumpResult, err := JumpToPathFromRoot(ctx, campaignRoot, relativePath)
		if err != nil {
			return "", err
		}
		return jumpResult.Path, nil
	}

	basePath := filepath.Join(campaignRoot, relativePath)
	exactPath := filepath.Join(basePath, query)
	if info, err := os.Stat(exactPath); err == nil && info.IsDir() {
		return exactPath, nil
	}

	if strings.Contains(query, "/") {
		parts := strings.SplitN(query, "/", 2)
		prefixPath, err := fuzzyResolveDirectory(ctx, basePath, parts[0], relativePath)
		if err != nil {
			return "", err
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		nestedPath := filepath.Join(prefixPath, parts[1])
		if info, err := os.Stat(nestedPath); err == nil && info.IsDir() {
			return nestedPath, nil
		}
		return "", camperrors.Wrapf(errNavigationPathNotFound, "%s/%s", strings.TrimRight(relativePath, "/"), query)
	}

	return fuzzyResolveDirectory(ctx, basePath, query, relativePath)
}

func fuzzyResolveDirectory(ctx context.Context, basePath, query, relativePath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", camperrors.Wrap(errNavigationPathNotFound, relativePath)
		}
		return "", camperrors.Wrap(err, "failed to read navigation path")
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}

	matches := fuzzy.FilterMulti(names, query)
	if len(matches) == 0 {
		return "", camperrors.Wrapf(errNavigationNoMatch, "%q in %s", query, strings.TrimRight(relativePath, "/"))
	}

	return filepath.Join(basePath, matches[0].Target), nil
}

var (
	errNavigationPathNotFound = camperrors.New("navigation path does not exist")
	errNavigationNoMatch      = camperrors.New("no directories match navigation query")
)
