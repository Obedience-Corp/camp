package artifacts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/peer"
)

// PullResult is the outcome of pulling one artifact root from a peer. JSON
// field names are camelCase to match the sync/v1alpha1 envelope this is
// embedded in.
type PullResult struct {
	Root   string `json:"root"`
	Policy string `json:"policy"`
	// Synced reports whether the rsync transfer completed (including a
	// partial transfer that rsync flagged with exit 23/24).
	Synced bool `json:"synced"`
	// FirstSync marks a pull with no prior snapshot for this peer: every
	// pre-existing local file was protected, because there is no baseline to
	// tell local edits from stale copies. Only new files arrived.
	FirstSync bool `json:"firstSync"`
	// Partial marks a transfer rsync reported as incomplete (exit 23/24);
	// files that did transfer are still snapshotted so they are not poisoned
	// into never-agreed protection on the next run.
	Partial bool `json:"partial,omitempty"`
	// Protected counts local files excluded from this pull (not overwritable:
	// modified since the last transfer from this peer, or never agreed with
	// it). SkippedConflicts lists the modified-since-baseline subset.
	Protected int `json:"protected"`
	// SkippedConflicts lists files changed locally since the last transfer
	// from this peer; they were excluded and left as-is, and stay protected
	// on future pulls until resolved (e.g. remove the local file to take the
	// peer's copy on the next sync). Media merge is a human decision.
	SkippedConflicts []string `json:"skippedConflicts,omitempty"`
	// Warning carries a non-fatal transfer problem; the sync itself proceeds.
	Warning string `json:"warning,omitempty"`
}

// excludeFromThreshold is where the exclude list moves from argv to a file:
// a pre-populated root on first sync protects every existing file, which can
// be far more paths than argv should carry.
const excludeFromThreshold = 20

// Pull transfers one declared artifact root from the peer, directionally and
// without deletions. rsync stages the delta into a scratch tree outside the
// live root (a --compare-dest delta transfer over the same ssh options as the
// rest of the peer stack), then each staged file is merged into place only
// after re-checking that the live destination still exactly matches the last
// agreed baseline. A file that differs from the baseline at merge time —
// including one a local writer touched during the transfer — is left
// untouched and reported, never clobbered. On success, the agreed state is
// snapshotted as the new baseline.
func Pull(ctx context.Context, campaignRoot string, src *peer.Source, root Root) *PullResult {
	result := &PullResult{Root: NormalizeRootPath(root.Path), Policy: root.EffectivePolicy()}
	if err := pull(ctx, campaignRoot, src, result); err != nil {
		result.Warning = err.Error()
	}
	return result
}

func pull(ctx context.Context, campaignRoot string, src *peer.Source, result *PullResult) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		return camperrors.New("rsync not found on PATH; artifact sync needs rsync on both machines")
	}

	// Re-validate the root at consume time: a hand-edited or malicious
	// artifacts.yaml never passed through Add, and a symlinked root must not
	// redirect the rsync destination outside the campaign.
	rootRel, err := EnsureRootWithin(campaignRoot, result.Root)
	if err != nil {
		return err
	}
	result.Root = rootRel
	destAbs := filepath.Join(campaignRoot, filepath.FromSlash(rootRel))
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		return camperrors.Wrapf(err, "create artifact root %s", rootRel)
	}

	// Serialize concurrent pulls of the SAME root: the staging dir, the live
	// merge, and the snapshot write are all keyed on the root, so two
	// simultaneous `camp sync --from` for one root would clear each other's
	// staging tree and interleave into an inconsistent baseline. A per-root
	// lock (stale-safe, 5s timeout) makes them queue; different roots still
	// pull concurrently. The lock lives under .campaign/cache (gitignored).
	cacheDir := filepath.Join(campaignRoot, ".campaign", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return camperrors.Wrap(err, "create cache dir")
	}
	release, err := fsutil.AcquireFileLock(ctx, filepath.Join(cacheDir, "artifact-pull-"+snapshotSlug(rootRel)+".lock"))
	if err != nil {
		return camperrors.Wrapf(err, "lock artifact root %s", rootRel)
	}
	defer release()

	local, err := BuildManifest(ctx, campaignRoot, rootRel)
	if err != nil {
		return err
	}
	baseline, err := LoadSnapshot(campaignRoot, src.ID(), rootRel)
	if err != nil {
		return err
	}

	protected := local.ProtectedPaths(baseline)
	result.Protected = len(protected)
	if baseline == nil {
		result.FirstSync = true
	} else {
		result.SkippedConflicts = modifiedSubset(protected, baseline)
	}

	// Stage the transfer outside the live root, then atomically merge each
	// file only after re-checking it against the baseline. A pre-transfer
	// exclude scan alone cannot give no-clobber semantics: a renderer can
	// write an unprotected path in the window between the walk and the write.
	// So rsync never touches the live tree directly.
	//
	// --compare-dest=<live root> keeps this a delta transfer: rsync compares
	// each source file against the live copy and only writes the ones that
	// changed or are new into the staging tree, so unchanged multi-GB media
	// is not re-copied. --no-links keeps peer symlinks from becoming phantom
	// local state. No --partial: rsync writes each file to a temp name and
	// renames on completion, and drops the in-flight temp on interruption, so
	// the staging tree only ever holds complete files (staging is per-pull and
	// wiped, so cross-run resume would buy nothing).
	stagingDir := filepath.Join(campaignRoot, ".campaign", "cache", "rsync-staging", snapshotSlug(rootRel))
	if err := os.RemoveAll(stagingDir); err != nil {
		return camperrors.Wrap(err, "clear rsync staging dir")
	}
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return camperrors.Wrap(err, "create rsync staging dir")
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	args := []string{"-a", "--no-links", "--compare-dest=" + destAbs}
	if sshCmd := src.SSHCommand(); sshCmd != "" {
		args = append(args, "-e", sshCmd)
	}
	excludeFile, excludeArgs, err := excludeArguments(protected)
	if err != nil {
		return err
	}
	if excludeFile != "" {
		defer func() { _ = os.Remove(excludeFile) }()
	}
	args = append(args, excludeArgs...)
	args = append(args, src.RsyncSpec(rootRel), stagingDir+string(os.PathSeparator))

	cmd := exec.CommandContext(ctx, "rsync", args...)
	output, runErr := cmd.CombinedOutput()

	staged, countErr := countFiles(stagingDir)
	if countErr != nil {
		return camperrors.Wrap(countErr, "scan rsync staging dir")
	}
	if runErr != nil {
		// rsync 23 (partial transfer due to error) and 24 (source files
		// vanished mid-transfer) are partial successes when files actually
		// staged; the SAME codes also cover "source root does not exist,"
		// where nothing staged. An empty staging tree on any error is the
		// hard failure it is.
		code := exitCode(runErr)
		if (code == 23 || code == 24) && staged > 0 {
			result.Partial = true
			result.Warning = fmt.Sprintf("partial transfer (rsync exit %d): %s", code, lastLines(string(output), 3))
		} else {
			return camperrors.Wrapf(runErr, "rsync %s from %s: %s", rootRel, src.ID(), lastLines(string(output), 3))
		}
	}

	// Atomically merge staged files into the live root, re-validating each
	// destination against the baseline immediately before the rename. A file
	// a local writer created or changed during the transfer no longer matches
	// the baseline and is left untouched (a late conflict), so no local byte
	// stream is ever silently overwritten.
	lateConflicts, err := mergeStaged(stagingDir, destAbs, baseline)
	if err != nil {
		return err
	}
	result.SkippedConflicts = appendUnique(result.SkippedConflicts, lateConflicts)
	result.Protected += len(lateConflicts)
	result.Synced = true

	after, err := BuildManifest(ctx, campaignRoot, rootRel)
	if err != nil {
		return camperrors.Wrap(err, "manifest after pull")
	}
	// Protect both the pre-scan protected set and any late conflicts so their
	// baseline stays sticky rather than being recorded as agreed.
	return SaveSnapshot(campaignRoot, src.ID(), rootRel,
		agreedState(after, baseline, append(append([]string{}, protected...), lateConflicts...)))
}

// mergeStaged moves each file from the staging tree into the live root, but
// only after re-checking that the live destination still exactly matches the
// baseline (or is absent). A destination that changed during the transfer is
// a late conflict: it is left as-is and reported, never overwritten. Renames
// are atomic because staging and the live root share the campaign filesystem.
func mergeStaged(stagingDir, destAbs string, baseline *Manifest) ([]string, error) {
	var base map[string]FileEntry
	if baseline != nil {
		base = baseline.Index()
	}
	// Resolve the root once so per-file destination checks can tell whether a
	// path stays inside it after symlink resolution.
	rootReal, err := filepath.EvalSymlinks(destAbs)
	if err != nil {
		rootReal = destAbs
	}
	var lateConflicts []string

	walkErr := filepath.WalkDir(stagingDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(stagingDir, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		destPath := filepath.Join(destAbs, rel)
		// A local symlinked directory component would let MkdirAll and the
		// rename follow it and write outside the campaign; safeToReplace also
		// reads every Lstat error as "absent, safe to write," which through a
		// symlinked parent means a non-existent escape target. Refuse and
		// report such a destination instead of following it.
		if !destWithinRoot(rootReal, destPath) || !safeToReplace(destAbs, rel, base) {
			lateConflicts = append(lateConflicts, relSlash)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return camperrors.Wrapf(err, "create dir for %s", relSlash)
		}
		if err := moveIntoPlace(path, destPath); err != nil {
			return camperrors.Wrapf(err, "merge %s into artifact root", relSlash)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return lateConflicts, nil
}

// destWithinRoot reports whether destPath resolves inside rootReal (the
// symlink-resolved artifact root). It resolves the existing prefix of the
// destination's parent, so a local symlinked directory component pointing
// outside the campaign is caught before MkdirAll or the rename can follow it.
func destWithinRoot(rootReal, destPath string) bool {
	resolved, err := resolveExistingPrefix(filepath.Dir(destPath))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootReal, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// moveIntoPlace atomically replaces dst with the staged file at src. The fast
// path is a rename, which is atomic on one filesystem. But an artifact root is
// commonly a mounted volume (large media on a dedicated disk) while staging
// lives under .campaign, so the rename can fail cross-device (EXDEV); in that
// case the staged bytes are copied into a temp file on the destination's own
// filesystem and that temp is atomically renamed into place, so the final swap
// is still atomic even though the copy is not.
func moveIntoPlace(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return err
	}
	return copyIntoPlace(src, dst)
}

// isCrossDevice reports whether err is a cross-filesystem link error (EXDEV).
func isCrossDevice(err error) bool {
	var le *os.LinkError
	if errors.As(err, &le) {
		return errors.Is(le.Err, syscall.EXDEV)
	}
	return errors.Is(err, syscall.EXDEV)
}

// copyIntoPlace copies src into a temp file beside dst (same filesystem) and
// atomically renames it over dst. It preserves mode and mtime so the next
// --compare-dest run does not see the file as changed and re-transfer it.
func copyIntoPlace(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".camp-artifact-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chtimes(tmpName, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	committed = true
	return nil
}

// safeToReplace reports whether the live file at rel may be overwritten: it is
// safe only when the live destination is absent, or present and identical to
// the recorded baseline. A file present locally but not in the baseline was
// created out-of-band (protect it); a file whose current state differs from
// the baseline was modified since the last transfer (a conflict). Detection
// uses size and nanosecond mtime, so on a filesystem whose mtime granularity
// cannot separate two writes, a same-size edit within one tick is the residual
// limit of the no-clobber guarantee.
func safeToReplace(destAbs, rel string, base map[string]FileEntry) bool {
	info, err := os.Lstat(filepath.Join(destAbs, filepath.FromSlash(rel)))
	if err != nil {
		return true // absent locally: nothing to clobber
	}
	b, ok := base[filepath.ToSlash(rel)]
	if !ok {
		return false // created locally out-of-band since the baseline
	}
	cur := FileEntry{
		Size:    info.Size(),
		MTime:   info.ModTime().UnixNano(),
		Symlink: info.Mode()&os.ModeSymlink != 0,
	}
	return sameEntry(cur, b)
}

// countFiles returns the number of regular files under dir, used to tell a
// partial transfer (some files staged) from a total failure (nothing staged).
func countFiles(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	return n, err
}

// appendUnique appends items to dst, skipping values already present.
func appendUnique(dst, items []string) []string {
	seen := make(map[string]bool, len(dst))
	for _, s := range dst {
		seen[s] = true
	}
	for _, s := range items {
		if !seen[s] {
			dst = append(dst, s)
			seen[s] = true
		}
	}
	return dst
}

// exitCode extracts a process exit code from an exec error, or -1 if the
// error is not an exit status (e.g. the binary could not start).
func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// agreedState builds the snapshot to record after a pull: files touched or
// verified by this transfer carry their current state, while protected files
// keep their previous baseline entry (or none, if they were never agreed).
// Recording a protected file's current local state would make the next pull
// treat it as agreed and silently overwrite it with the peer's copy; this
// keeps conflict protection sticky until the human resolves the file.
func agreedState(after, baseline *Manifest, protected []string) *Manifest {
	protectedSet := make(map[string]bool, len(protected))
	for _, p := range protected {
		protectedSet[p] = true
	}
	var base map[string]FileEntry
	if baseline != nil {
		base = baseline.Index()
	}

	snap := &Manifest{Version: 1, Root: after.Root, GeneratedAt: time.Now().UTC()}
	for _, f := range after.Files {
		if !protectedSet[f.Path] {
			snap.Files = append(snap.Files, f)
			continue
		}
		if b, ok := base[f.Path]; ok {
			snap.Files = append(snap.Files, b)
		}
	}
	return snap
}

// modifiedSubset filters the protected list down to paths the baseline knows
// about: files changed since the last transfer, i.e. genuine conflicts worth
// reporting. Never-agreed local files are protected too but are not
// conflicts.
func modifiedSubset(protected []string, baseline *Manifest) []string {
	base := baseline.Index()
	var modified []string
	for _, p := range protected {
		if _, ok := base[p]; ok {
			modified = append(modified, p)
		}
	}
	return modified
}

// excludeArguments renders the protected paths as rsync exclude flags, or as
// an --exclude-from temp file when the list is large. Patterns are anchored
// to the transfer root and glob metacharacters in real filenames are escaped.
func excludeArguments(protected []string) (tempFile string, args []string, err error) {
	if len(protected) == 0 {
		return "", nil, nil
	}
	patterns := make([]string, len(protected))
	for i, p := range protected {
		patterns[i] = "/" + escapeRsyncPattern(p)
	}
	if len(patterns) <= excludeFromThreshold {
		for _, p := range patterns {
			args = append(args, "--exclude="+p)
		}
		return "", args, nil
	}

	f, err := os.CreateTemp("", "camp-artifact-excludes-*.txt")
	if err != nil {
		return "", nil, camperrors.Wrap(err, "create exclude file")
	}
	if _, err := f.WriteString(strings.Join(patterns, "\n") + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, camperrors.Wrap(err, "write exclude file")
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, camperrors.Wrap(err, "close exclude file")
	}
	return f.Name(), []string{"--exclude-from=" + f.Name()}, nil
}

// escapeRsyncPattern escapes rsync glob metacharacters so a literal filename
// is matched literally. Backslash is escaped first (and thus mapped by the
// single Replacer pass, not re-processed) so a filename containing a
// backslash before a glob char does not produce an active wildcard.
func escapeRsyncPattern(p string) string {
	r := strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `*`, `\*`, `?`, `\?`)
	return r.Replace(p)
}

// lastLines trims rsync output to its tail, where the actionable error lives.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
