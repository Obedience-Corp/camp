package dungeon

import (
	"context"
	"errors"
	"os"
	"path/filepath"

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
		candidate := filepath.Join(dir, "dungeon")
		info, statErr := os.Stat(candidate)
		switch {
		case statErr == nil && info.IsDir():
			return Context{
				DungeonPath: candidate,
				ParentPath:  dir,
			}, nil
		case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
			return Context{}, camperrors.Wrapf(statErr, "stat %s", candidate)
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
