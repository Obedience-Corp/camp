package nav

import (
	"context"
	"os"
	"path/filepath"

	"github.com/obediencecorp/camp/internal/campaign"
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
