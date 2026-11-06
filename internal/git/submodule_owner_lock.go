package git

import (
	"context"
	"path/filepath"
	"sync"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// SubmoduleOwnerLock serializes submodule operations which share a superproject's
// config, index, and modules directory. A worker may still perform independent
// work before acquiring it. The lock is process-local; Git's own lockfiles
// protect against external processes and are never removed here.
type SubmoduleOwnerLock struct {
	mu     sync.Mutex
	owners map[string]chan struct{}
}

var submoduleOwners SubmoduleOwnerLock

// Lock waits for exclusive use of an owning repository. The returned release
// function must be called once, including when the operation fails.
func (l *SubmoduleOwnerLock) Lock(ctx context.Context, repoDir string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolve submodule owner %q", repoDir)
	}
	owner, err = filepath.EvalSymlinks(owner)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolve submodule owner %q", repoDir)
	}
	l.mu.Lock()
	if l.owners == nil {
		l.owners = make(map[string]chan struct{})
	}
	sem := l.owners[owner]
	if sem == nil {
		sem = make(chan struct{}, 1)
		l.owners[owner] = sem
	}
	l.mu.Unlock()
	select {
	case sem <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-sem
			return nil, err
		}
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func LockSubmoduleOwner(ctx context.Context, repoDir string) (func(), error) {
	return submoduleOwners.Lock(ctx, repoDir)
}
