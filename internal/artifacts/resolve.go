package artifacts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/peer"
)

// Conflict resolution (D005).
//
// A pull refuses to overwrite a local file whose bytes are not exactly the
// last state agreed with that peer. That refusal is sticky: it survives every
// later sync, which is what makes "camp never loses your local bytes" true
// rather than aspirational. Before this command the only way out was to delete
// the local file so it stopped being protected — destroying data to express a
// preference.
//
// Resolve is the intended exit, and the only thing in camp allowed to move a
// baseline across a conflict. Both directions preserve the invariant rather
// than suspending it:
//
//   - take-peer fetches the peer's bytes for that one path, puts them in
//     place, and records them as agreed. Local and baseline now match the
//     peer, so the conflict is genuinely gone rather than muted.
//
//   - take-local keeps the local bytes and REMOVES the path from the baseline
//     instead of writing the local entry into it. That distinction is the
//     whole correctness of this command. Recording the local entry as agreed
//     would make local match the baseline, which is precisely the condition a
//     pull treats as safe to overwrite — so the next sync would replace the
//     file the user just chose to keep, one command after they chose it.
//     Removing the entry leaves the file protected (unknown provenance) and
//     therefore untouchable, while dropping it out of the reported-conflict
//     set, because conflicts are the protected paths the baseline knows.
//
// The cost of take-local is stated rather than hidden: that path is pinned
// local for this peer, so later peer changes to it will not arrive on their
// own. Taking the peer's copy later is another resolve away.

// ResolveAction is which side of a conflict the user chose.
type ResolveAction string

const (
	// ResolveTakeLocal keeps the local bytes and stops the pull from ever
	// overwriting them from this peer.
	ResolveTakeLocal ResolveAction = "take-local"
	// ResolveTakePeer replaces the local bytes with the peer's copy and
	// records them as the agreed baseline.
	ResolveTakePeer ResolveAction = "take-peer"
)

// Conflict is one reported artifact conflict: a local file that changed since
// the last state agreed with a peer.
type Conflict struct {
	// Root is the declared artifact root, relative to the campaign.
	Root string `json:"root"`
	// Path is the file's path relative to Root.
	Path string `json:"path"`
	// Peer is the machine id whose baseline this conflicts with.
	Peer string `json:"peer"`
	// LocalSize and LocalMTime describe the file as it is on disk now.
	LocalSize  int64 `json:"localSize"`
	LocalMTime int64 `json:"localMtimeUnixNano"`
	// AgreedSize and AgreedMTime describe the last state agreed with the peer,
	// which is the version a pull would otherwise have replaced it with.
	AgreedSize  int64 `json:"agreedSize"`
	AgreedMTime int64 `json:"agreedMtimeUnixNano"`
	// AgreedAt is when that baseline was recorded. camp does not persist a
	// separate conflict-discovery time, and this is the closest honest
	// answer: the conflict has existed since some point after this.
	AgreedAt time.Time `json:"agreedAt"`
}

// ResolveResult reports one resolution.
type ResolveResult struct {
	Root   string        `json:"root"`
	Path   string        `json:"path"`
	Peer   string        `json:"peer"`
	Action ResolveAction `json:"action"`
	// OldBaseline is the agreed entry before the resolution, nil when absent.
	OldBaseline *FileEntry `json:"oldBaseline,omitempty"`
	// NewBaseline is the agreed entry afterwards. nil for take-local, which
	// removes the entry so the file stays protected.
	NewBaseline *FileEntry `json:"newBaseline,omitempty"`
	// PinnedLocal marks a take-local: the path is now protected from this peer
	// indefinitely, so later peer changes will not arrive on their own.
	PinnedLocal bool `json:"pinnedLocal,omitempty"`
}

// Conflicts lists every open conflict for peerID across the campaign's
// declared artifact roots. It is the dry-run for resolve: seeing what is stuck
// requires no mutation.
func Conflicts(ctx context.Context, campaignRoot, peerID string) ([]Conflict, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := ValidatePeerID(peerID); err != nil {
		return nil, err
	}
	cfg, err := Load(campaignRoot)
	if err != nil {
		return nil, err
	}

	var conflicts []Conflict
	for _, root := range cfg.Roots {
		rootRel, err := EnsureRootWithin(campaignRoot, root.Path)
		if err != nil {
			continue
		}
		found, err := rootConflicts(ctx, campaignRoot, peerID, rootRel)
		if err != nil {
			return nil, err
		}
		conflicts = append(conflicts, found...)
	}
	return conflicts, nil
}

// rootConflicts lists conflicts for one artifact root.
func rootConflicts(ctx context.Context, campaignRoot, peerID, rootRel string) ([]Conflict, error) {
	baseline, err := LoadSnapshot(campaignRoot, peerID, rootRel)
	if err != nil || baseline == nil {
		// No baseline means nothing has been agreed with this peer yet, so
		// nothing can be in conflict with it.
		return nil, nil
	}
	local, err := BuildManifest(ctx, campaignRoot, rootRel)
	if err != nil {
		return nil, camperrors.Wrapf(err, "manifest for %s", rootRel)
	}

	localIdx := local.Index()
	baseIdx := baseline.Index()
	var conflicts []Conflict
	for _, p := range modifiedSubset(local.ProtectedPaths(baseline), baseline) {
		l, agreed := localIdx[p], baseIdx[p]
		conflicts = append(conflicts, Conflict{
			Root: rootRel, Path: p, Peer: peerID,
			LocalSize: l.Size, LocalMTime: l.MTime,
			AgreedSize: agreed.Size, AgreedMTime: agreed.MTime,
			AgreedAt: baseline.GeneratedAt,
		})
	}
	return conflicts, nil
}

// findConflict locates the single conflict for path, or explains precisely why
// there is none. The error text matters: "no conflict" is the answer to three
// different situations and a user who cannot tell them apart cannot act.
func findConflict(ctx context.Context, campaignRoot, peerID, path string) (Conflict, error) {
	all, err := Conflicts(ctx, campaignRoot, peerID)
	if err != nil {
		return Conflict{}, err
	}
	want := NormalizeRootPath(path)
	for _, c := range all {
		if c.Path == want || filepath.ToSlash(filepath.Join(c.Root, c.Path)) == want {
			return c, nil
		}
	}
	return Conflict{}, noConflictError(peerID, want, all)
}

// noConflictError distinguishes "that path is fine", "camp has never heard of
// that path", and "there are no conflicts at all with this peer".
func noConflictError(peerID, want string, all []Conflict) error {
	if len(all) == 0 {
		return camperrors.Newf("no open conflicts with peer %q; nothing to resolve", peerID)
	}
	open := make([]string, 0, len(all))
	for _, c := range all {
		open = append(open, filepath.ToSlash(filepath.Join(c.Root, c.Path)))
	}
	return camperrors.Newf("%q is not an open conflict with peer %q (open: %v)", want, peerID, open)
}

// Resolve applies the user's choice to one conflicted path.
//
// src is required only for take-peer, which has to fetch bytes; take-local is
// a local decision and must work with the peer unreachable, because "keep what
// I have" should never depend on the machine you are disagreeing with.
func Resolve(ctx context.Context, campaignRoot string, src *peer.Source, peerID, path string, action ResolveAction) (*ResolveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	conflict, err := findConflict(ctx, campaignRoot, peerID, path)
	if err != nil {
		return nil, err
	}

	release, err := lockRoot(ctx, campaignRoot, conflict.Root)
	if err != nil {
		return nil, err
	}
	defer release()

	baseline, err := LoadSnapshot(campaignRoot, peerID, conflict.Root)
	if err != nil {
		return nil, err
	}
	if baseline == nil {
		return nil, camperrors.Newf("baseline for %s disappeared while resolving", conflict.Root)
	}
	old := baseline.Index()[conflict.Path]

	result := &ResolveResult{
		Root: conflict.Root, Path: conflict.Path, Peer: peerID, Action: action,
		OldBaseline: &old,
	}

	switch action {
	case ResolveTakeLocal:
		if err := applyTakeLocal(campaignRoot, peerID, baseline, conflict); err != nil {
			return nil, err
		}
		result.PinnedLocal = true
	case ResolveTakePeer:
		entry, err := applyTakePeer(ctx, campaignRoot, src, peerID, baseline, conflict)
		if err != nil {
			return nil, err
		}
		result.NewBaseline = entry
	default:
		return nil, camperrors.Newf("unknown resolve action %q", action)
	}
	return result, nil
}

// applyTakeLocal drops the path from the baseline so the local file stays
// protected. See the package comment: writing the local entry in instead would
// hand the next pull permission to overwrite it.
func applyTakeLocal(campaignRoot, peerID string, baseline *Manifest, c Conflict) error {
	kept := make([]FileEntry, 0, len(baseline.Files))
	for _, f := range baseline.Files {
		if f.Path != c.Path {
			kept = append(kept, f)
		}
	}
	baseline.Files = kept
	baseline.GeneratedAt = time.Now().UTC()
	return SaveSnapshot(campaignRoot, peerID, c.Root, baseline)
}

// applyTakePeer fetches the peer's copy of the one conflicted path, puts it in
// place, and records it as agreed.
//
// The fetch is staged beside the root and renamed in, so an interrupted
// resolve leaves the local file exactly as it was rather than truncated: the
// command that exists to protect a user's bytes must not be the one that
// shreds them.
func applyTakePeer(ctx context.Context, campaignRoot string, src *peer.Source, peerID string, baseline *Manifest, c Conflict) (*FileEntry, error) {
	if src == nil {
		return nil, camperrors.New("--take-peer needs a reachable peer; pass --from <machine>")
	}
	destAbs := filepath.Join(campaignRoot, filepath.FromSlash(c.Root))
	destPath := filepath.Join(destAbs, filepath.FromSlash(c.Path))

	staging, err := os.MkdirTemp(filepath.Dir(destAbs), ".camp-resolve-")
	if err != nil {
		return nil, camperrors.Wrap(err, "create resolve staging dir")
	}
	defer func() { _ = os.RemoveAll(staging) }()
	staged := filepath.Join(staging, filepath.Base(c.Path))

	if err := fetchOnePath(ctx, src, c, staged); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return nil, camperrors.Wrapf(err, "create dir for %s", c.Path)
	}
	if err := moveIntoPlace(staged, destPath); err != nil {
		return nil, camperrors.Wrapf(err, "install peer copy of %s", c.Path)
	}

	entry, err := entryFor(ctx, destAbs, c.Path)
	if err != nil {
		return nil, err
	}
	baseline.Files = append(withoutPath(baseline.Files, c.Path), entry)
	baseline.GeneratedAt = time.Now().UTC()
	if err := SaveSnapshot(campaignRoot, peerID, c.Root, baseline); err != nil {
		return nil, err
	}
	return &entry, nil
}

// fetchOnePath copies exactly one file from the peer, over the same ssh
// options every other peer operation uses.
func fetchOnePath(ctx context.Context, src *peer.Source, c Conflict, dest string) error {
	remote := src.RsyncSpec(c.Root)
	remote = remote[:len(remote)-1] + "/" + c.Path // RsyncSpec ends in "/"

	args := []string{"-a", "-s", "--no-links"}
	if sshCmd := src.SSHCommand(); sshCmd != "" {
		args = append(args, "-e", sshCmd)
	}
	args = append(args, remote, dest)

	out, err := exec.CommandContext(ctx, "rsync", args...).CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "fetch %s from %s: %s", c.Path, src.ID(), lastLines(string(out), 2))
	}
	return nil
}

// withoutPath returns entries with path removed.
func withoutPath(files []FileEntry, path string) []FileEntry {
	kept := make([]FileEntry, 0, len(files))
	for _, f := range files {
		if f.Path != path {
			kept = append(kept, f)
		}
	}
	return kept
}

// entryFor builds the manifest entry describing a file as it now exists.
func entryFor(ctx context.Context, rootAbs, rel string) (FileEntry, error) {
	full := filepath.Join(rootAbs, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil {
		return FileEntry{}, camperrors.Wrapf(err, "stat %s after resolve", rel)
	}
	entry := FileEntry{
		Path:    rel,
		Size:    info.Size(),
		MTime:   info.ModTime().UnixNano(),
		Symlink: info.Mode()&os.ModeSymlink != 0,
	}
	if !entry.Symlink {
		var hashed int64
		hash, err := hashFile(ctx, full, &hashed, nil)
		if err != nil {
			return FileEntry{}, err
		}
		entry.HashSHA256 = hash
	}
	return entry, nil
}

// lockRoot takes the same per-root lock a pull uses, so a resolve and a sync
// of the same root cannot interleave into an inconsistent baseline.
func lockRoot(ctx context.Context, campaignRoot, rootRel string) (func(), error) {
	cacheDir := filepath.Join(campaignRoot, ".campaign", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, camperrors.Wrap(err, "create cache dir")
	}
	return fsutil.AcquireFileLock(ctx, filepath.Join(cacheDir, "artifact-pull-"+snapshotSlug(rootRel)+".lock"))
}
