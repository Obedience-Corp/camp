package artifacts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
// without deletions (rsync -a --partial over the same ssh options as the
// rest of the peer stack). A local file is only overwritable when its state
// exactly matches the last agreed baseline with this peer; everything else
// is excluded and reported, never clobbered. On success, the agreed state is
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

	// --partial-dir keeps interrupted-transfer bytes OUT of the destination
	// tree (a bare --partial would leave a truncated file at the real name,
	// which the post-pull manifest would then record as an agreed baseline
	// and refuse to repair). The partial dir lives under .campaign/cache
	// (gitignored, same filesystem as the root so completed files rename
	// atomically) and is never walked by BuildManifest.
	// --no-links: never materialize a peer symlink locally (phantom state no
	// snapshot could track); local symlinks are recorded in the manifest and
	// so are protected like any other entry.
	// --update: the protected set is computed from a walk that finished before
	// this transfer starts, so a file written locally in the window between
	// the walk and rsync is not in the exclude list. --update skips any
	// destination that is newer than the peer's copy, which is exactly the
	// signature of a file a local writer just touched, so a concurrent edit is
	// not clobbered. This narrows but does not fully eliminate the race (a
	// concurrent write landing an older-or-equal mtime is still possible on a
	// truly live tree); the design's guarantee assumes a quiescent root.
	partialDir := filepath.Join(campaignRoot, ".campaign", "cache", "rsync-partial")
	if err := os.MkdirAll(partialDir, 0o755); err != nil {
		return camperrors.Wrap(err, "create rsync partial dir")
	}
	args := []string{"-a", "--no-links", "--update", "--partial-dir=" + partialDir}
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
	args = append(args, src.RsyncSpec(rootRel), destAbs+string(os.PathSeparator))

	cmd := exec.CommandContext(ctx, "rsync", args...)
	output, runErr := cmd.CombinedOutput()

	after, mErr := BuildManifest(ctx, campaignRoot, rootRel)
	if mErr != nil {
		return camperrors.Wrap(mErr, "manifest after pull")
	}

	if runErr != nil {
		// rsync 23 (partial transfer due to error) and 24 (source files
		// vanished mid-transfer) can be partial successes: the files that DID
		// arrive are valid, so snapshotting them avoids poisoning them into
		// never-agreed protection on the next run. But the SAME exit codes
		// also cover "source root does not exist," where nothing transferred.
		// Distinguish by whether any complete file actually changed locally
		// (--partial-dir keeps incomplete files out of the tree, so an
		// unchanged tree means nothing landed) and treat a no-op as the hard
		// failure it is.
		code := exitCode(runErr)
		if (code == 23 || code == 24) && !manifestsEqual(local, after) {
			result.Partial = true
			result.Warning = fmt.Sprintf("partial transfer (rsync exit %d): %s", code, lastLines(string(output), 3))
		} else {
			return camperrors.Wrapf(runErr, "rsync %s from %s: %s", rootRel, src.ID(), lastLines(string(output), 3))
		}
	}
	result.Synced = true

	return SaveSnapshot(campaignRoot, src.ID(), rootRel, agreedState(after, baseline, protected))
}

// manifestsEqual reports whether two manifests describe the same set of files
// with the same versions, so a failed rsync that changed nothing can be told
// apart from a partial transfer that landed at least one complete file.
func manifestsEqual(a, b *Manifest) bool {
	if len(a.Files) != len(b.Files) {
		return false
	}
	bi := b.Index()
	for _, f := range a.Files {
		g, ok := bi[f.Path]
		if !ok || !sameEntry(f, g) {
			return false
		}
	}
	return true
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
