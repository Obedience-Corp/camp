package clone

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// Cold seed by pack copy (phase 003_COLD_SEED). A `git clone` from a peer
// still makes the peer's git walk history and build a pack per repository;
// for a fresh machine seeding a whole campaign that work is pure overhead,
// because the peer already has the bytes packed on disk. This path copies
// those bytes in bulk and then hands the result to git.
//
// What makes it safe is not the copy, it is what surrounds it:
//
//   - Only immutable content is copied — the object store and refs. Never the
//     index, never config or hooks, never a working tree. That is the class A
//     invariant: bytes that cannot change under us while we read them.
//   - A repository is only eligible if the quiescence contract said so (D001),
//     and the same HEAD is re-verified after the copy, so a peer that moved
//     during the transfer is a detected condition rather than a torn clone.
//   - Connectivity is checked against the copied bytes alone, before any
//     origin fetch, so an incomplete copy is attributed to the copy instead of
//     being silently papered over by objects fetched from origin.
//   - Completion is entirely git's: origin is re-pointed to the real URL, the
//     delta is fetched, and the checkout is a normal git checkout. However the
//     bytes arrived, the end state is an origin replica.
//
// Anything that disqualifies a repository returns a reason rather than an
// error, because the caller's response is to fall back, not to abort.

// packCopySubdirs are the parts of a git directory a cold seed copies. Both
// hold content that is immutable once written: object files are named by their
// own hash, and a ref file is replaced atomically by rename rather than edited
// in place. Everything else in a git directory — the index, config, hooks,
// logs, worktree state — is live and is never copied.
var packCopySubdirs = []string{"objects", "refs"}

// packCopyFiles are the individual files copied alongside packCopySubdirs.
//
// packed-refs is not optional in practice: git compacts refs out of refs/ into
// it, and a peer whose refs have been packed has an empty refs/ tree, so a copy
// without it arrives with no branches at all.
//
// HEAD is what makes a copied submodule adoptable. `git submodule update`
// reuses an existing .git/modules/<name> instead of cloning, but it cannot
// check anything out if HEAD names a branch the copy does not have. Both are
// ref pointers, written by atomic rename like everything else under refs/.
var packCopyFiles = []string{"packed-refs", "HEAD"}

// PackSeedOutcome reports how one repository was handled by the cold-seed
// path, so the caller can both act on it and explain it to the user.
type PackSeedOutcome struct {
	// Repo is the repository path relative to the campaign root ("." = root).
	Repo string
	// Seeded reports whether pack bytes were copied and accepted.
	Seeded bool
	// FallbackReason explains why the copy did not happen or was discarded.
	// Empty when Seeded. Its presence is the signal to use the bundle path.
	FallbackReason string
}

// packCopyEligible reports whether a verdict authorises a byte copy, and if
// not, why. The reason is user-facing: it is what explains the fallback.
//
// The git directory is checked for containment here rather than trusted. It
// arrives from the peer, and camp is about to read from it, so an exotic or
// hostile layout (a --separate-git-dir pointing outside the campaign) makes
// the repository ineligible rather than making camp read an arbitrary path.
// That is deliberately not a protocol error: such a repo is unusual, not
// malformed, and the bundle path handles it correctly.
func packCopyEligible(v RepoVerdict, peerRoot string) (bool, string) {
	if !v.Quiescent {
		reason := "peer repository is not quiescent"
		if len(v.Reasons) > 0 {
			reason += ": " + strings.Join(v.Reasons, "; ")
		}
		return false, reason
	}
	if v.HeadSHA == "" {
		return false, "peer reported no HEAD to verify the copy against"
	}
	if v.GitDir == "" {
		return false, "peer reported no git directory to copy from"
	}
	if !withinRoot(peerRoot, v.GitDir) {
		return false, "peer git directory " + v.GitDir + " is outside the camp root"
	}
	return true, ""
}

// withinRoot reports whether abs is root itself or a path beneath it. Both are
// absolute peer paths, so this is lexical containment after cleaning; there is
// no local filesystem to resolve them against.
func withinRoot(root, abs string) bool {
	if !filepath.IsAbs(abs) || !filepath.IsAbs(root) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(abs))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// copyPackBytes transfers one repository's immutable object and ref bytes from
// the peer's git directory into destGitDir.
//
// Transfers are resumable: --partial-dir keeps an interrupted file so a retry
// continues it instead of restarting. That is safe precisely because of what
// is being copied — a pack file's name contains the hash of its own contents,
// so a partial file can only ever be completed into the same bytes, never into
// different ones. (The artifact pull deliberately does the opposite and drops
// partials, because its staging tree is wiped per run and mutable user files
// have no such guarantee.)
func (c *Cloner) copyPackBytes(ctx context.Context, peerGitDir, destGitDir string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		return camperrors.New("rsync not found on PATH")
	}
	if err := os.MkdirAll(destGitDir, 0o755); err != nil {
		return camperrors.Wrap(err, "create destination git dir")
	}

	for _, sub := range packCopySubdirs {
		src := c.peer.RsyncSpecAbs(filepath.Join(peerGitDir, sub) + "/")
		dest := filepath.Join(destGitDir, sub) + string(os.PathSeparator)
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return camperrors.Wrapf(err, "create %s dir", sub)
		}
		if err := c.runPackRsync(ctx, src, dest); err != nil {
			return camperrors.Wrapf(err, "copy %s", sub)
		}
	}

	// packed-refs may legitimately not exist; rsync exit 23 covers "source
	// vanished", which for a single named file is the same as absent.
	for _, name := range packCopyFiles {
		src := c.peer.RsyncSpecAbs(filepath.Join(peerGitDir, name))
		if err := c.runPackRsync(ctx, src, destGitDir+string(os.PathSeparator)); err != nil {
			if exitCodeOf(err) == 23 {
				continue
			}
			return camperrors.Wrapf(err, "copy %s", name)
		}
	}
	return nil
}

// runPackRsync executes one rsync leg with camp's shared ssh options, so the
// copy rides the same connection and identity as every other peer hop rather
// than standing up a second ssh stack.
func (c *Cloner) runPackRsync(ctx context.Context, src, dest string) error {
	args := []string{"-a", "-s", "--no-links", "--partial-dir=.camp-partial"}
	if sshCmd := c.peer.SSHCommand(); sshCmd != "" {
		args = append(args, "-e", sshCmd)
	}
	args = append(args, src, dest)

	cmd := exec.CommandContext(ctx, "rsync", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return camperrors.NewCommand("rsync", exitCodeOf(err), lastLine(string(output)), err)
	}
	return nil
}

// exitCodeOf extracts a process exit code from err, or 0 when it is not an
// exit failure.
func exitCodeOf(err error) int {
	var cmdErr *camperrors.CommandError
	if errors.As(err, &cmdErr) {
		return cmdErr.ExitCode
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 0
}

// lastLine returns the final non-empty line of output, which is where rsync
// and git put the message that actually explains a failure.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// verifyCopiedConnectivity walks every object reachable from the copied refs
// and fails if any is missing. It runs before the origin fetch on purpose: an
// incomplete copy must be attributed to the copy, not quietly repaired by
// objects pulled from origin afterwards.
//
// rev-list rather than fsck: this needs reachability, which is what a torn
// copy breaks, and it does not need fsck's full object re-hashing over a
// repository that may be gigabytes.
func verifyCopiedConnectivity(ctx context.Context, dir string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-list", "--objects", "--all")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "connectivity check on copied objects: %s", lastLine(string(output)))
	}
	return nil
}

// verifyHeadUnmoved re-reads the peer's verdict for repo and confirms HEAD is
// still what it was when the copy was authorised. This is the post-copy
// re-verify from D001: it converts the window between check and copy from a
// silent corruption risk into a fallback.
func (c *Cloner) verifyHeadUnmoved(ctx context.Context, repo, wantHead string) error {
	report, err := CheckQuiescence(ctx, c.peer)
	if err != nil {
		return camperrors.Wrapf(err, "re-verify peer after copying %s", repo)
	}
	v, ok := report.Verdict(repo)
	if !ok {
		return camperrors.New("peer no longer reports " + repo + "; treating the copy as untrusted")
	}
	if v.HeadSHA != wantHead {
		return camperrors.Newf("peer %s moved during the copy (HEAD %s -> %s)",
			repo, shortSHA(wantHead), shortSHA(v.HeadSHA))
	}
	if !v.Quiescent {
		return camperrors.New("peer " + repo + " stopped being quiescent during the copy: " +
			strings.Join(v.Reasons, "; "))
	}
	return nil
}

// shortSHA abbreviates a commit for a message without hiding which one it is.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// packSeedRoot cold-seeds the campaign root repository from the peer's bytes
// and completes it with git. It returns the absolute target directory.
//
// A partial destination is removed on any failure, matching gitCloneFromPeer:
// the caller's response to a failed seed is to clone from origin, and that
// cannot run if a half-built directory is in the way.
func (c *Cloner) packSeedRoot(ctx context.Context, v RepoVerdict) (string, error) {
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

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(targetDir)
		}
	}()

	if out, err := exec.CommandContext(ctx, "git", "init", "-q", targetDir).CombinedOutput(); err != nil {
		return "", camperrors.Wrapf(err, "init destination: %s", lastLine(string(out)))
	}
	if err := c.copyPackBytes(ctx, v.GitDir, filepath.Join(targetDir, ".git")); err != nil {
		return "", err
	}
	if err := c.verifyHeadUnmoved(ctx, v.Repo, v.HeadSHA); err != nil {
		return "", err
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

// cloneRootFromPeer seeds the campaign root from the peer, preferring the
// pack copy and degrading to a git clone from the peer when the copy is not
// available. Both are peer transports; the caller's own fallback to origin
// sits behind this one, so a cold seed degrades in two steps rather than
// jumping straight to the network.
//
// The quiescence check is what gates the fast path, and a failure to reach or
// parse the peer only costs the fast path — it is recorded as a warning, and
// the git-clone-from-peer route still runs.
func (c *Cloner) cloneRootFromPeer(ctx context.Context, result *CloneResult) (string, error) {
	report, err := CheckQuiescence(ctx, c.peer)
	if err != nil {
		reason := fmt.Sprintf("quiescence check failed: %v", err)
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("quiescence check on %s failed, cloning from peer instead of copying: %v", c.peer.ID(), err))
		dir, cloneErr := c.gitCloneFromPeer(ctx)
		return c.recordRootSeed(result, SeedMethodPeerClone, reason, dir, cloneErr)
	}
	c.peerQuiescence = report

	v, found := report.Verdict(quiescenceRootRepo)
	if !found {
		const reason = "peer reported no verdict for the camp root"
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("peer %s reported no verdict for the camp root; cloning from peer", c.peer.ID()))
		dir, cloneErr := c.gitCloneFromPeer(ctx)
		return c.recordRootSeed(result, SeedMethodPeerClone, reason, dir, cloneErr)
	}
	fallbackReason := ""
	if ok, reason := packCopyEligible(v, report.Root); ok {
		c.progress.Message(fmt.Sprintf("Cold-seeding camp root from %s", c.peer.ID()))
		dir, err := c.packSeedRoot(ctx, v)
		if err == nil {
			return c.recordRootSeed(result, SeedMethodPackCopy, "", dir, nil)
		}
		fallbackReason = fmt.Sprintf("copy failed: %v", err)
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("cold-seed copy of the camp root failed (%v); bundling from peer instead", err))
	} else {
		fallbackReason = reason
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("cold-seed copy skipped for the camp root (%s); bundling from peer instead", reason))
	}

	// The peer is being written to, or the copy did not hold up. A bundle is
	// written in one pass and verified before it is read, so it is correct
	// regardless of what the peer is doing.
	c.progress.Message(fmt.Sprintf("Bundling camp root from %s", c.peer.ID()))
	dir, err := c.bundleSeedRoot(ctx, report.Root)
	if err == nil {
		return c.recordRootSeed(result, SeedMethodBundle, fallbackReason, dir, nil)
	}
	result.Warnings = append(result.Warnings,
		fmt.Sprintf("bundle seed of the camp root failed (%v); cloning from peer", err))
	cloneDir, cloneErr := c.gitCloneFromPeer(ctx)
	return c.recordRootSeed(result, SeedMethodPeerClone, fmt.Sprintf("bundle failed: %v", err), cloneDir, cloneErr)
}

// recordRootSeed notes the transport that delivered the campaign root and
// passes the clone result through, so every return path records exactly once.
func (c *Cloner) recordRootSeed(result *CloneResult, method, reason string, dir string, err error) (string, error) {
	if err != nil {
		// The caller falls back to origin; that outcome is recorded there.
		return dir, err
	}
	result.Seed = append(result.Seed, SeedRepoResult{Repo: quiescenceRootRepo, Method: method, Reason: reason})
	return dir, nil
}

// errColdSeedSkipped marks a submodule the cold-seed copy declined to handle —
// no verdict, or a verdict that did not authorise a copy. It is separated from
// a copy that was attempted and failed because only the latter is worth telling
// the user about: declining is the contract working, failing is news.
var errColdSeedSkipped = errors.New("cold seed not applicable")

// coldSeedSubmodule runs the pack copy for one submodule when the peer's
// quiescence report authorises it. The report is the one collected during the
// root clone, so this costs no extra round-trip and every submodule is judged
// against the same snapshot of the peer.
func (c *Cloner) coldSeedSubmodule(ctx context.Context, repoDir string, sub SubmoduleInfo) (string, string, error) {
	if c.peerQuiescence == nil {
		return "", "", errColdSeedSkipped
	}
	v, found := c.peerQuiescence.Verdict(sub.Path)
	if !found {
		return "", "", errColdSeedSkipped
	}
	reason := ""
	if ok, why := packCopyEligible(v, c.peerQuiescence.Root); ok {
		if err := c.packSeedSubmodule(ctx, repoDir, sub, v); err == nil {
			return SeedMethodPackCopy, "", nil
		} else {
			reason = fmt.Sprintf("copy failed: %v", err)
		}
		// Fall through: a copy that failed is exactly the case the bundle
		// exists for, and it costs one more attempt before the network.
	} else {
		reason = why
	}
	if err := c.bundleSeedSubmodule(ctx, repoDir, sub, c.peerQuiescence.Root); err != nil {
		return "", reason, err
	}
	return SeedMethodBundle, reason, nil
}

// packSeedSubmodule seeds one submodule's object store from the peer's bytes
// before git initialises it, so `git submodule update` finds the recorded
// commit already present and does no network transfer.
//
// The destination is the module directory git itself will use
// (.git/modules/<name>). `git submodule update --init` reuses an existing
// module directory rather than cloning into it, so populating it first means
// the subsequent init transfers nothing: it registers the path and checks the
// recorded commit straight out of the copied objects.
func (c *Cloner) packSeedSubmodule(ctx context.Context, repoDir string, sub SubmoduleInfo, v RepoVerdict) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := pathutil.ValidateSubmodulePath(repoDir, sub.Path); err != nil {
		return camperrors.Wrapf(err, "submodule path %q", sub.Path)
	}

	moduleDir := filepath.Join(repoDir, ".git", "modules", sub.Name)
	if _, err := os.Stat(moduleDir); err == nil {
		return camperrors.New("module dir already exists for " + sub.Path)
	}

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(moduleDir)
		}
	}()

	if out, err := exec.CommandContext(ctx, "git", "init", "-q", "--bare", moduleDir).CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "init module dir for %s: %s", sub.Path, lastLine(string(out)))
	}
	if err := c.copyPackBytes(ctx, v.GitDir, moduleDir); err != nil {
		return err
	}
	if err := c.verifyHeadUnmoved(ctx, v.Repo, v.HeadSHA); err != nil {
		return err
	}
	if err := completeSeededModule(ctx, moduleDir); err != nil {
		return err
	}
	success = true
	return nil
}

// completeSeededRoot finishes a seeded campaign root, whichever transport
// delivered the objects. Connectivity is checked first, on the delivered bytes
// alone, so an incomplete delivery is attributed to the transport rather than
// repaired invisibly by the origin fetch that follows.
func (c *Cloner) completeSeededRoot(ctx context.Context, targetDir string) error {
	if err := verifyCopiedConnectivity(ctx, targetDir); err != nil {
		return err
	}
	// A seeded root either has no origin yet (init) or has the transport's
	// own URL (clone from a bundle); add-then-set-url covers both without
	// caring which happened.
	addRemote := exec.CommandContext(ctx, "git", "-C", targetDir, "remote", "add", "origin", c.options.URL)
	if out, err := addRemote.CombinedOutput(); err != nil && !strings.Contains(string(out), "already exists") {
		return camperrors.Wrapf(err, "add origin: %s", lastLine(string(out)))
	}
	return c.repointOrigin(ctx, targetDir)
}

// completeSeededModule finishes a seeded submodule git directory so
// `git submodule update --init` adopts it instead of cloning: connectivity
// verified, and core.bare cleared so git can attach a working tree.
func completeSeededModule(ctx context.Context, moduleDir string) error {
	if err := verifyCopiedConnectivity(ctx, moduleDir); err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "git", "-C", moduleDir,
		"config", "core.bare", "false").CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "clear core.bare: %s", lastLine(string(out)))
	}
	return nil
}
