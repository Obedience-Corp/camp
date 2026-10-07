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

// missingRootID names the one notice about every absent root at once, so it
// has no subject.
const missingRootID = "artifact-roots-missing-locally"

// ArtifactRootNeverSynced reports a declared root that has never left this
// machine.
//
// This is the notice that justifies the surface. Declaring a root moves its
// bytes out of git's care and into sync's, and until a sync actually runs
// there is exactly one copy of them anywhere. The user believes the
// declaration protected the data; it did not, yet.
//
// A second copy is proven two ways. Another machine's committed manifest
// records files under the root, which is the signal that reaches this machine
// when the other one pulled from it: that pull happens over there, and the
// manifest comes back through git. Or this machine has a non-empty snapshot
// from pulling the root off a peer. An empty record proves nothing in either
// case: a machine without the root commits an empty manifest, and pulling from
// a peer whose root is empty snapshots nothing agreed.
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
	self, others := otherManifestMachines(campaignRoot)

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
		id := SubjectID(KindNeverSynced, rel)
		if dismissals.IsDismissed(id) {
			continue
		}
		if hasPopulatedSnapshot(campaignRoot, peers, rel) || anotherMachineHolds(campaignRoot, others, rel) {
			continue
		}
		return &Notice{
			ID:      id,
			Subject: rel,
			Message: fmt.Sprintf(
				"%s is a declared artifact root that has never synced; its contents exist on this machine only",
				rel),
			Command: "on another machine: camp sync --from " + pullSourceHint(self) +
				" --artifacts-only   (dismiss: camp notify dismiss " + id + ")",
		}, nil
	}
	return nil, nil
}

// otherManifestMachines returns this machine's manifest identity and every
// other machine that has committed manifests. Without an identity, no
// manifest can be told apart from this machine's own, so none is counted. An
// unreadable manifest tree counts none either: it can only keep the notice up,
// never hide it.
func otherManifestMachines(campaignRoot string) (string, []string) {
	self, err := artifacts.MachineName()
	if err != nil {
		return "", nil
	}
	machines, err := artifacts.ListManifestMachines(campaignRoot)
	if err != nil {
		return self, nil
	}
	others := make([]string, 0, len(machines))
	for _, m := range machines {
		if m != self {
			others = append(others, m)
		}
	}
	return self, others
}

// anotherMachineHolds reports whether another machine's committed manifest
// records files under a root. One file read per machine: no hashing, no walk.
func anotherMachineHolds(campaignRoot string, others []string, rel string) bool {
	for _, machine := range others {
		m, _, err := artifacts.LoadCommitted(campaignRoot, machine, rel)
		if err == nil && m != nil && len(m.Files) > 0 {
			return true
		}
	}
	return false
}

// pullSourceHint names this machine for the command run on another one. The id
// that machine knows this one by is its own choice in its machines file, so
// the hostname is offered as a hint rather than printed as though it were that
// id.
func pullSourceHint(self string) string {
	if self == "" {
		return "<this-machine-id>"
	}
	return "<id of " + self + ">"
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
		id := SubjectID(KindManifestDrift, rel)
		if dismissals.IsDismissed(id) {
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
		return &Notice{
			ID:      id,
			Subject: rel,
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

// hasPopulatedSnapshot reports whether a pull from any peer agreed on at least
// one file under a root. One read per peer per root: no hashing, no walking
// the root itself.
func hasPopulatedSnapshot(campaignRoot string, peers []string, rel string) bool {
	for _, peer := range peers {
		if snap, err := artifacts.LoadSnapshot(campaignRoot, peer, rel); err == nil && snap != nil && len(snap.Files) > 0 {
			return true
		}
	}
	return false
}
