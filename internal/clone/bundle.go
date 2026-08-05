package clone

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// The bundle fallback is the cold-seed path for repositories the quiescence
// contract would not clear. A bundle is a single file the peer's git writes in
// one pass and camp reads only after `git bundle verify` accepts it, so there
// is no window in which a half-delivered result can be mistaken for a whole
// one — it is transactional by construction rather than by checking. That is
// what makes it the right answer for a peer that is being written to, and it
// is why it is worth its cost, which is the pack CPU on the peer that the
// copy path exists to avoid.
//
// Both paths converge: whatever delivered the objects, completion runs the
// same helpers and ends in the same origin replica.

// bundleRefspecs map a bundle's refs into the destination. Heads and tags are
// taken verbatim; the bundle is a snapshot of the peer, and origin is re-pointed
// and re-fetched afterwards, which prunes anything origin does not have.
var bundleRefspecs = []string{
	"+refs/heads/*:refs/heads/*",
	"+refs/tags/*:refs/tags/*",
}

// peerRepoPath returns the working path of one repository on the peer.
// "." addresses the campaign root itself.
func peerRepoPath(peerRoot, repo string) string {
	if repo == quiescenceRootRepo || repo == "" {
		return peerRoot
	}
	return path.Join(peerRoot, repo)
}

// createPeerBundle asks the peer's git to write a bundle of one repository and
// streams it into destFile. `--all` carries every ref plus HEAD, so the bundle
// is self-describing and the destination needs nothing else to lay out a repo.
func (c *Cloner) createPeerBundle(ctx context.Context, peerRoot, repo, destFile string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f, err := os.Create(destFile)
	if err != nil {
		return camperrors.Wrap(err, "create bundle file")
	}
	defer func() { _ = f.Close() }()

	script := "git -C " + remote.ShellQuote(peerRepoPath(peerRoot, repo)) + " bundle create - --all"
	if err := c.peer.StreamShell(ctx, script, f); err != nil {
		return camperrors.Wrapf(err, "bundle %s on peer %s", repo, c.peer.ID())
	}
	if err := f.Close(); err != nil {
		return camperrors.Wrap(err, "flush bundle file")
	}
	return verifyBundle(ctx, destFile)
}

// verifyBundle refuses a bundle git cannot vouch for. This is the transaction
// boundary: a truncated stream fails here, before anything reads objects out
// of it, so a partial transfer can never be mistaken for a complete seed.
func verifyBundle(ctx context.Context, file string) error {
	out, err := exec.CommandContext(ctx, "git", "bundle", "verify", file).CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "bundle failed verification: %s", lastLine(string(out)))
	}
	return nil
}

// bundleScratchDir makes a scratch directory for a bundle on the same
// filesystem as its destination, so the transfer is not silently redirected
// onto a small tmpfs and the bytes never cross a device boundary.
func bundleScratchDir(near string) (string, func(), error) {
	dir, err := os.MkdirTemp(near, ".camp-bundle-")
	if err != nil {
		return "", nil, camperrors.Wrap(err, "create bundle scratch dir")
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// bundleSeedRoot cold-seeds the campaign root from a bundle the peer writes.
// git clones from the bundle, so the layout, refs, and HEAD are git's own work;
// completion is then identical to the copy path.
func (c *Cloner) bundleSeedRoot(ctx context.Context, peerRoot string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	targetDir := c.options.Directory
	if targetDir == "" {
		targetDir = extractRepoName(c.options.URL)
	}
	if _, err := os.Stat(targetDir); err == nil {
		return "", camperrors.Newf("target directory %s already exists", targetDir)
	}

	parent := filepath.Dir(targetDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", camperrors.Wrap(err, "create target parent dir")
	}
	scratch, cleanup, err := bundleScratchDir(parent)
	if err != nil {
		return "", err
	}
	defer cleanup()

	bundleFile := filepath.Join(scratch, "root.bundle")
	if err := c.createPeerBundle(ctx, peerRoot, quiescenceRootRepo, bundleFile); err != nil {
		return "", err
	}

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(targetDir)
		}
	}()

	if out, err := exec.CommandContext(ctx, "git", "clone", "--quiet", bundleFile, targetDir).
		CombinedOutput(); err != nil {
		return "", camperrors.Wrapf(err, "clone from bundle: %s", lastLine(string(out)))
	}
	if err := c.completeSeededRoot(ctx, targetDir); err != nil {
		return "", err
	}

	success = true
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return targetDir, nil
	}
	return absDir, nil
}

// bundleSeedSubmodule seeds one submodule's git directory from a bundle, so a
// non-quiescent submodule still arrives without touching origin.
//
// `git clone --bare` from the bundle is what lays the directory out, including
// HEAD; completeSeededModule then clears core.bare so `git submodule update`
// adopts the directory instead of cloning into it. The module's origin is
// re-pointed to the declared URL because the bundle it was cloned from is a
// scratch file that is about to be deleted.
func (c *Cloner) bundleSeedSubmodule(ctx context.Context, repoDir string, sub SubmoduleInfo, peerRoot string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	moduleDir := filepath.Join(repoDir, ".git", "modules", sub.Name)
	if _, err := os.Stat(moduleDir); err == nil {
		return camperrors.New("module dir already exists for " + sub.Path)
	}

	scratch, cleanup, err := bundleScratchDir(filepath.Join(repoDir, ".git"))
	if err != nil {
		return err
	}
	defer cleanup()

	bundleFile := filepath.Join(scratch, "sub.bundle")
	if err := c.createPeerBundle(ctx, peerRoot, sub.Path, bundleFile); err != nil {
		return err
	}

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(moduleDir)
		}
	}()

	if err := os.MkdirAll(filepath.Dir(moduleDir), 0o755); err != nil {
		return camperrors.Wrap(err, "create modules dir")
	}
	if out, err := exec.CommandContext(ctx, "git", "clone", "--quiet", "--bare", bundleFile, moduleDir).
		CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "clone submodule %s from bundle: %s", sub.Path, lastLine(string(out)))
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", moduleDir,
		"remote", "set-url", "origin", sub.URL).CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "re-point %s origin: %s", sub.Path, lastLine(string(out)))
	}
	if err := completeSeededModule(ctx, moduleDir); err != nil {
		return err
	}

	success = true
	return nil
}
