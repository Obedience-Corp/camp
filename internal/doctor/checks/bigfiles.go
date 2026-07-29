package checks

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/doctor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/stageguard"
)

// BigFilesCheck finds large files the commit path structurally cannot see.
//
// Two blind spots motivate it. Files already gitignored never appear in
// git status, so the staging guard cannot detect them at all — and that is the
// most common real state: the user gitignored their media long ago, and it now
// moves by no mechanism whatsoever, since git skips it and no artifact root
// claims it. Second, blobs already committed are past the guard by definition;
// the guard stops the next one, not the last one.
//
// Solving either at commit time would mean walking the disk on every commit.
// This check is where that cost belongs, which is why it is opt-in.
type BigFilesCheck struct{}

// NewBigFilesCheck creates a new large-file sweep.
func NewBigFilesCheck() *BigFilesCheck {
	return &BigFilesCheck{}
}

// ID returns the check identifier.
func (c *BigFilesCheck) ID() string {
	return "bigfiles"
}

// Name returns the human-readable check name.
func (c *BigFilesCheck) Name() string {
	return "Large Files"
}

// Description returns a brief explanation of what this check does.
func (c *BigFilesCheck) Description() string {
	return "Finds large files owned by no system, and large blobs already in git history"
}

// Detail keys and ownership states.
const (
	bigFilesDetailPath  = "path"
	bigFilesDetailSize  = "size"
	bigFilesDetailState = "state"

	// stateTrackedInHistory is a blob already committed. Removing it requires
	// a history rewrite, which is a human decision camp will not make.
	stateTrackedInHistory = "tracked_in_history"
	// stateUnowned is a large file on disk that git ignores and no artifact
	// root, LFS filter, or DVC pointer claims. It moves by no mechanism.
	stateUnowned = "owned_by_no_system"
)

// Run performs both passes.
func (c *BigFilesCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &doctor.CheckResult{
		Passed:  true,
		Issues:  make([]doctor.Issue, 0),
		Details: make(map[string]any),
	}

	// The guard's own thresholds, so doctor and commit never disagree about
	// what "large" means. A user who raises the limit raises it everywhere.
	limits, err := stageguard.ResolveLimits(ctx, repoRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve large-file thresholds")
	}
	result.Details["threshold_bytes"] = limits.MaxFileSize

	tracked, err := trackedBlobsAtHEAD(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	for _, blob := range tracked {
		if blob.size <= limits.MaxFileSize {
			continue
		}
		result.Issues = append(result.Issues, bigFileIssue(c.ID(), blob.path, blob.size,
			stateTrackedInHistory,
			fmt.Sprintf("%s is %s in git history", blob.path, humanBytes(blob.size)),
			""))
	}

	unowned, err := c.unownedLargeFiles(ctx, repoRoot, limits, tracked)
	if err != nil {
		return nil, err
	}
	for _, f := range unowned {
		result.Issues = append(result.Issues, bigFileIssue(c.ID(), f.path, f.size,
			stateUnowned,
			fmt.Sprintf("%s (%s) is owned by no system: git ignores it and no artifact root claims it",
				f.path, humanBytes(f.size)),
			"camp artifacts add "+filepath.ToSlash(filepath.Dir(f.path))))
	}

	result.Total = len(tracked) + len(unowned)
	// Detection only. Both states are reported as warnings because neither is
	// something camp will repair: a history rewrite is a human decision, and
	// adopting an unowned directory is a choice about what should sync.
	return result, nil
}

// Fix is a no-op. This check never remediates.
//
// Removing a blob from history rewrites it, and deciding that an unowned
// directory should become an artifact root changes what syncs between
// machines. Both are the user's call; camp's job here is to make the state
// visible, which it could not do at commit time.
func (c *BigFilesCheck) Fix(ctx context.Context, repoPath string, issues []doctor.Issue) ([]doctor.Issue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, nil
}

// sizedPath is one file with its size.
type sizedPath struct {
	path string
	size int64
}

// trackedBlobsAtHEAD lists every blob at HEAD with its size, in one git call.
func trackedBlobsAtHEAD(ctx context.Context, repoRoot string) ([]sizedPath, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "ls-tree", "-r", "-l", "-z", "HEAD")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// A repository with no commits yet has no HEAD; that is not a fault,
		// it just means the tracked pass has nothing to report.
		return nil, nil
	}

	var blobs []sizedPath
	for _, record := range bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0}) {
		if len(record) == 0 {
			continue
		}
		blob, ok := parseLsTreeLongRecord(string(record))
		if ok {
			blobs = append(blobs, blob)
		}
	}
	return blobs, nil
}

// parseLsTreeLongRecord parses one `ls-tree -l` record:
//
//	<mode> SP <type> SP <object> SP <size> TAB <path>
//
// Size is "-" for entries that are not blobs (submodule gitlinks, trees).
func parseLsTreeLongRecord(record string) (sizedPath, bool) {
	tab := strings.IndexByte(record, '\t')
	if tab < 0 {
		return sizedPath{}, false
	}
	meta, path := record[:tab], record[tab+1:]

	fields := strings.Fields(meta)
	if len(fields) < 4 || fields[1] != "blob" {
		return sizedPath{}, false
	}
	size, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return sizedPath{}, false
	}
	return sizedPath{path: filepath.ToSlash(path), size: size}, true
}

// unownedLargeFiles walks the campaign for large files no system claims.
func (c *BigFilesCheck) unownedLargeFiles(
	ctx context.Context,
	repoRoot string,
	limits stageguard.GuardLimits,
	tracked []sizedPath,
) ([]sizedPath, error) {
	trackedSet := make(map[string]bool, len(tracked))
	for _, t := range tracked {
		trackedSet[t.path] = true
	}

	cfg, err := artifacts.Load(repoRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "load artifact declarations")
	}
	roots := make([]string, 0, len(cfg.Roots))
	for _, r := range cfg.Roots {
		if n := artifacts.NormalizeRootPath(r.Path); n != "" {
			roots = append(roots, n)
		}
	}

	var found []sizedPath
	walkErr := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable subtree is not worth failing the sweep
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if shouldSkipDir(rel, roots) {
				return filepath.SkipDir
			}
			return nil
		}
		if trackedSet[rel] || rel == "." {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil || info.Size() <= limits.MaxFileSize {
			return nil
		}
		if stageguard.MatchesAllow(rel, limits.Allow) {
			return nil
		}
		if _, governed := dvcGoverned(repoRoot, rel); governed {
			return nil
		}
		found = append(found, sizedPath{path: rel, size: info.Size()})
		return nil
	})
	if walkErr != nil {
		return nil, camperrors.Wrap(walkErr, "walk campaign for large files")
	}

	return filterLFSManaged(ctx, repoRoot, found), nil
}

// shouldSkipDir reports directories the sweep must not descend into.
//
// A declared artifact root is skipped because sync already owns its contents:
// reporting them would tell the user to fix something they have already fixed,
// which is how a check trains people to ignore it.
func shouldSkipDir(rel string, roots []string) bool {
	if rel == "." {
		return false
	}
	base := filepath.Base(rel)
	if base == ".git" {
		return true
	}
	// Camp's own state tree is never user content. Skipping it is not a
	// convenience: .campaign holds machine-local derived state (the graph
	// database, peer snapshots, caches) that is gitignored and claimed by no
	// artifact root, so the sweep would classify it as "owned by no system"
	// and suggest `camp artifacts add .campaign` — a remedy ValidateRootPath
	// refuses outright. A finding whose only fix is rejected is worse than no
	// finding: it teaches the user this check is wrong.
	if rel == artifacts.CampaignStateDir || strings.HasPrefix(rel, artifacts.CampaignStateDir+"/") {
		return true
	}
	for _, root := range roots {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

// filterLFSManaged drops paths git-LFS already governs.
func filterLFSManaged(ctx context.Context, repoRoot string, files []sizedPath) []sizedPath {
	if len(files) == 0 {
		return files
	}
	var stdin bytes.Buffer
	for _, f := range files {
		stdin.WriteString(f.path)
		stdin.WriteByte(0)
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "check-attr", "-z", "--stdin", "filter")
	cmd.Stdin = &stdin
	out, err := cmd.Output()
	if err != nil {
		return files // cannot ask; report what we have rather than dropping it
	}

	managed := make(map[string]bool)
	fields := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	for i := 0; i+2 < len(fields); i += 3 {
		if string(fields[i+2]) == "lfs" {
			managed[string(fields[i])] = true
		}
	}

	kept := make([]sizedPath, 0, len(files))
	for _, f := range files {
		if !managed[f.path] {
			kept = append(kept, f)
		}
	}
	return kept
}

// bigFileIssue builds one finding with its machine-readable details.
func bigFileIssue(checkID, path string, size int64, state, desc, fixCmd string) doctor.Issue {
	return doctor.Issue{
		Severity:    doctor.SeverityWarning,
		CheckID:     checkID,
		Description: desc,
		FixCommand:  fixCmd,
		AutoFixable: false,
		Details: map[string]any{
			bigFilesDetailPath:  path,
			bigFilesDetailSize:  size,
			bigFilesDetailState: state,
		},
	}
}

// humanBytes renders a size for a finding description.
func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
