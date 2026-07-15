package dungeon

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// Resolver errors.
var (
	ErrDungeonContextNotFound = camperrors.Wrap(camperrors.ErrNotFound, "dungeon context not found")
	ErrInvalidDungeonContext  = camperrors.Wrap(camperrors.ErrInvalidInput, "invalid dungeon context")
)

// Context identifies the resolved dungeon directory and its owning parent path.
type Context struct {
	DungeonPath string
	ParentPath  string
}

// ResolveContext finds the nearest dungeon directory by walking from cwd up to
// campaignRoot (inclusive). Returns the first "<dir>/dungeon" that exists.
func ResolveContext(ctx context.Context, campaignRoot, cwd string) (Context, error) {
	if err := ctx.Err(); err != nil {
		return Context{}, camperrors.Wrap(err, "context cancelled")
	}

	if campaignRoot == "" {
		return Context{}, camperrors.Wrap(ErrInvalidDungeonContext, "empty campaign root")
	}
	if cwd == "" {
		return Context{}, camperrors.Wrap(ErrInvalidDungeonContext, "empty working directory")
	}

	absRoot, err := filepath.Abs(campaignRoot)
	if err != nil {
		return Context{}, camperrors.Wrap(err, "resolving campaign root")
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return Context{}, camperrors.Wrap(err, "resolving current working directory")
	}

	if err := pathutil.ValidateBoundary(absRoot, absCwd); err != nil {
		return Context{}, camperrors.Wrap(
			camperrors.NewBoundary("resolve_dungeon_context", absCwd, absRoot, err),
			"working directory outside campaign root",
		)
	}

	root, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return Context{}, camperrors.Wrap(err, "resolving campaign root symlinks")
	}
	dir, err := filepath.EvalSymlinks(absCwd)
	if err != nil {
		return Context{}, camperrors.Wrap(err, "resolving working directory symlinks")
	}

	for {
		if err := ctx.Err(); err != nil {
			return Context{}, camperrors.Wrap(err, "context cancelled")
		}

		resolved, err := spelling.Resolve(ctx, dir)
		if err != nil {
			return Context{}, camperrors.Wrapf(err, "resolving dungeon spelling under %s", dir)
		}
		if resolved.Exists && !isFestOwned(root, dir) {
			spelling.WarnIfConflicting(os.Stderr, resolved)
			return Context{
				DungeonPath: resolved.Path,
				ParentPath:  dir,
			}, nil
		}

		if dir == root {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return Context{}, camperrors.Wrapf(
		ErrDungeonContextNotFound,
		"no dungeon found walking from %s to %s",
		absCwd,
		absRoot,
	)
}

func isFestOwned(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return false
	}
	for _, elem := range splitPathElements(rel) {
		if elem == "festivals" {
			return true
		}
	}
	return false
}

func splitPathElements(p string) []string {
	parts := strings.Split(filepath.Clean(p), string(filepath.Separator))
	elems := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			elems = append(elems, part)
		}
	}
	return elems
}
