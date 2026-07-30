package notice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/artifacts"
)

// The forward-looking artifact notices: states camp itself created, surfaced
// on a command the user already runs.
//
// The line these detectors hold is that a notice describes something the user
// may not know is true, and is dismissible. A report of an action camp just
// took is neither, and lives in the commit path instead. The backward-looking
// case — a large file gitignored years ago and owned by nothing — is
// deliberately absent here: a deliberate gitignore is not a defect to nag
// about on every status, and `camp doctor -c bigfiles` is its surface.
//
// Every detector below is stat-level over the declared-root list. None scans
// the campaign tree, so a campaign with no declared roots does no work at all.

// Notice ID prefixes. The root path is part of the ID so a newly declared root
// produces a new signature and notifies even if an older one was dismissed.
const (
	neverSyncedIDPrefix = "artifact-root-never-synced:"
	missingRootID       = "artifact-roots-missing-locally"
)

// ArtifactRootNeverSynced reports a declared root that has never left this
// machine.
//
// This is the notice that justifies the surface. Declaring a root moves its
// bytes out of git's care and into sync's, and until a sync actually runs
// there is exactly one copy of them anywhere. The user believes the
// declaration protected the data; it did not, yet.
func ArtifactRootNeverSynced(ctx context.Context, campaignRoot string) (*Notice, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	cfg, err := artifacts.Load(campaignRoot)
	if err != nil || len(cfg.Roots) == 0 {
		return nil, err
	}

	peers, err := artifacts.ListSnapshotPeers(campaignRoot)
	if err != nil {
		return nil, err
	}

	// Dismissals are consulted here rather than left to the caller's filter.
	// A detector reports at most one notice, so filtering afterward would let
	// a dismissal on the first root suppress every root behind it: the user
	// silences one and never hears about the next one they declare. Per
	// signature has to mean the detector skips signatures already answered.
	dismissals, err := LoadDismissals(campaignRoot)
	if err != nil {
		dismissals = &DismissalFile{}
	}

	for _, root := range cfg.Roots {
		rel := artifacts.NormalizeRootPath(root.Path)
		if rel == "" || !rootExists(campaignRoot, rel) {
			continue
		}
		if hasAnySnapshot(campaignRoot, peers, rel) {
			continue
		}
		if dismissals.IsDismissed(neverSyncedIDPrefix + rel) {
			continue
		}
		id := neverSyncedIDPrefix + rel
		return &Notice{
			ID: id,
			Message: fmt.Sprintf(
				"%s is a declared artifact root that has never synced; its contents exist on this machine only",
				rel),
			Command: "camp sync --from <machine>   (dismiss: camp notify dismiss " + id + ")",
		}, nil
	}
	return nil, nil
}

// ArtifactRootsMissingLocally reports declared roots absent from this machine.
//
// Same fact `camp artifacts list` shows, surfaced without being asked. It is
// one line naming a count rather than a list, because the actionable answer is
// the same for all of them and a per-root enumeration would crowd the command
// the user actually ran.
func ArtifactRootsMissingLocally(ctx context.Context, campaignRoot string) (*Notice, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	cfg, err := artifacts.Load(campaignRoot)
	if err != nil || len(cfg.Roots) == 0 {
		return nil, err
	}

	missing := 0
	for _, root := range cfg.Roots {
		rel := artifacts.NormalizeRootPath(root.Path)
		if rel != "" && !rootExists(campaignRoot, rel) {
			missing++
		}
	}
	if missing == 0 {
		return nil, nil
	}

	plural := "roots are"
	if missing == 1 {
		plural = "root is"
	}
	return &Notice{
		ID:      missingRootID,
		Message: fmt.Sprintf("%d declared artifact %s not on this machine", missing, plural),
		Command: "camp sync --from <machine>   (dismiss: camp notify dismiss " + missingRootID + ")",
	}, nil
}

// ArtifactRootDrift reports a declared root whose contents no longer match its
// committed manifest.
//
// Stat-level by contract: size, nanosecond mtime, and presence, bounded by the
// declared-root list, no hashing on the status path. The notice reports and
// never resolves; the manifest is a record of what was, and the next commit is
// what updates it.
func ArtifactRootDrift(ctx context.Context, campaignRoot string) (*Notice, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	cfg, err := artifacts.Load(campaignRoot)
	if err != nil || len(cfg.Roots) == 0 {
		return nil, err
	}
	machine, err := artifacts.MachineName()
	if err != nil {
		return nil, nil // no identity, no record to compare against
	}
	dismissals, err := LoadDismissals(campaignRoot)
	if err != nil {
		dismissals = &DismissalFile{}
	}

	for _, root := range cfg.Roots {
		rel := artifacts.NormalizeRootPath(root.Path)
		if rel == "" || !rootExists(campaignRoot, rel) {
			continue
		}
		if dismissals.IsDismissed(manifestDriftIDPrefix + rel) {
			continue
		}
		committed, _, err := artifacts.LoadCommitted(campaignRoot, machine, rel)
		if err != nil || committed == nil {
			continue // no committed record yet; never-synced covers the gap
		}
		drifts, err := artifacts.DetectDrift(ctx, campaignRoot, committed)
		if err != nil || len(drifts) == 0 {
			continue
		}
		id := manifestDriftIDPrefix + rel
		return &Notice{
			ID: id,
			Message: fmt.Sprintf(
				"%s has drifted from its committed manifest (%d paths); the record no longer matches this machine",
				rel, len(drifts)),
			Command: "camp commit   (dismiss: camp notify dismiss " + id + ")",
		}, nil
	}
	return nil, nil
}

// rootExists reports whether a declared root is present on this machine.
func rootExists(campaignRoot, rel string) bool {
	_, err := os.Stat(filepath.Join(campaignRoot, filepath.FromSlash(rel)))
	return err == nil
}

// hasAnySnapshot reports whether any peer has ever recorded a transfer for a
// root. One stat per peer per root: no hashing, no walking the root itself.
func hasAnySnapshot(campaignRoot string, peers []string, rel string) bool {
	for _, peer := range peers {
		if snap, err := artifacts.LoadSnapshot(campaignRoot, peer, rel); err == nil && snap != nil {
			return true
		}
	}
	return false
}

// manifestDriftIDPrefix keys drift dismissals per root, so silencing one
// root's drift does not silence the next root that drifts.
const manifestDriftIDPrefix = "artifact-manifest-drift:"
