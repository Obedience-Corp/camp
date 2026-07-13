package artifacts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/peer"
)

// PullResult is the outcome of pulling one artifact root from a peer.
type PullResult struct {
	Root   string `json:"root"`
	Policy string `json:"policy"`
	// Synced reports whether the rsync transfer completed.
	Synced bool `json:"synced"`
	// FirstSync marks a pull with no prior snapshot for this peer: every
	// pre-existing local file was protected, because there is no baseline to
	// tell local edits from stale copies. Only new files arrived.
	FirstSync bool `json:"first_sync"`
	// Protected counts local files excluded from this pull (not overwritable:
	// modified since the last transfer from this peer, or never agreed with
	// it). SkippedConflicts lists the modified-since-baseline subset.
	Protected int `json:"protected"`
	// SkippedConflicts lists files changed locally since the last transfer
	// from this peer; they were excluded and left as-is, and stay protected
	// on future pulls until resolved (e.g. remove the local file to take the
	// peer's copy on the next sync). Media merge is a human decision.
	SkippedConflicts []string `json:"skipped_conflicts,omitempty"`
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

	rootRel := result.Root
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

	args := []string{"-a", "--partial"}
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
	if output, err := cmd.CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "rsync %s from %s: %s", rootRel, src.ID(), lastLines(string(output), 3))
	}
	result.Synced = true

	after, err := BuildManifest(ctx, campaignRoot, rootRel)
	if err != nil {
		return camperrors.Wrap(err, "manifest after pull")
	}
	return SaveSnapshot(campaignRoot, src.ID(), rootRel, agreedState(after, baseline, protected))
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
// is matched literally.
func escapeRsyncPattern(p string) string {
	r := strings.NewReplacer(`[`, `\[`, `]`, `\]`, `*`, `\*`, `?`, `\?`)
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
