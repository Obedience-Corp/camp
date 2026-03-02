package nav

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// DirectJumpResult contains the result of a direct jump operation.
type DirectJumpResult struct {
	// Path is the absolute path to jump to.
	Path string
	// Category is the resolved category.
	Category Category
	// IsRoot indicates if jumping to campaign root.
	IsRoot bool
}

// DirectJump resolves a category to its absolute path within the campaign.
// If category is CategoryAll (empty), returns the campaign root.
func DirectJump(ctx context.Context, cat Category) (*DirectJumpResult, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, err
	}

	// No category = jump to campaign root
	if cat == CategoryAll {
		return &DirectJumpResult{
			Path:     root,
			Category: cat,
			IsRoot:   true,
		}, nil
	}

	dir := cat.Dir()
	absPath := filepath.Join(root, dir)

	// Verify directory exists
	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		return nil, &DirectJumpError{
			Category: cat,
			Path:     absPath,
			Err:      ErrCategoryNotFound,
		}
	}
	if err != nil {
		return nil, &DirectJumpError{
			Category: cat,
			Path:     absPath,
			Err:      err,
		}
	}
	if !info.IsDir() {
		return nil, &DirectJumpError{
			Category: cat,
			Path:     absPath,
			Err:      ErrNotADirectory,
		}
	}

	return &DirectJumpResult{
		Path:     absPath,
		Category: cat,
		IsRoot:   false,
	}, nil
}

// DirectJumpFromRoot resolves a category to its absolute path using a known root.
// This is more efficient when the root is already known.
func DirectJumpFromRoot(ctx context.Context, root string, cat Category) (*DirectJumpResult, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// No category = jump to campaign root
	if cat == CategoryAll {
		return &DirectJumpResult{
			Path:     root,
			Category: cat,
			IsRoot:   true,
		}, nil
	}

	dir := cat.Dir()
	absPath := filepath.Join(root, dir)

	// Verify directory exists
	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		return nil, &DirectJumpError{
			Category: cat,
			Path:     absPath,
			Err:      ErrCategoryNotFound,
		}
	}
	if err != nil {
		return nil, &DirectJumpError{
			Category: cat,
			Path:     absPath,
			Err:      err,
		}
	}
	if !info.IsDir() {
		return nil, &DirectJumpError{
			Category: cat,
			Path:     absPath,
			Err:      ErrNotADirectory,
		}
	}

	return &DirectJumpResult{
		Path:     absPath,
		Category: cat,
		IsRoot:   false,
	}, nil
}

// JumpToPath resolves a relative path to an absolute path within the campaign.
// The path is relative to the campaign root and must exist.
func JumpToPath(ctx context.Context, relativePath string) (*DirectJumpResult, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, err
	}

	return JumpToPathFromRoot(ctx, root, relativePath)
}

// JumpToPathFromRoot resolves a relative path to an absolute path using a known root.
// This is more efficient when the root is already known.
func JumpToPathFromRoot(ctx context.Context, root string, relativePath string) (*DirectJumpResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	absPath := filepath.Join(root, relativePath)

	// Verify directory exists
	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("path does not exist: %s", relativePath)
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "failed to stat path %s", relativePath)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", relativePath)
	}

	return &DirectJumpResult{
		Path:     absPath,
		Category: CategoryAll, // Custom paths don't map to categories
		IsRoot:   false,
	}, nil
}
