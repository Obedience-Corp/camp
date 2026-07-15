package artifacts

import (
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Snapshots are the manifest recorded after the last successful transfer
// from a given peer, stored under .campaign/cache (gitignored, machine-local
// derived state). They exist so the next pull can tell "changed locally
// since we last synced with that peer" from "safe to overwrite": conflict
// detection needs a baseline, and this is it. Losing a snapshot is safe; the
// next pull just degrades to first-sync caution (--ignore-existing).

const snapshotDir = ".campaign/cache/peersync"

// snapshotPath returns the snapshot file for one (peer, root) pair.
func snapshotPath(campaignRoot, peerID, rootRel string) string {
	return filepath.Join(campaignRoot, filepath.FromSlash(snapshotDir), peerID, snapshotSlug(rootRel)+".json")
}

// snapshotSlug encodes a root path into a single, injective, filesystem-safe
// filename component. A raw "/"->"__" replace is not injective (the roots
// "a/b" and "a__b" collapse onto one baseline file, so syncing one poisons
// the other's baseline). Percent-escaping the escape byte first and then the
// separator keeps distinct roots distinct while staying human-readable.
func snapshotSlug(rootRel string) string {
	norm := NormalizeRootPath(rootRel)
	norm = strings.ReplaceAll(norm, "%", "%25")
	norm = strings.ReplaceAll(norm, "/", "%2F")
	return norm
}

// ValidatePeerID rejects peer identifiers that could escape the snapshot tree
// (peer ids are joined into .campaign/cache/peersync/<peer>/...). Ids resolved
// from the machines file are already constrained, but `--from` in verify mode
// is raw user input read straight into a filesystem path.
func ValidatePeerID(id string) error {
	if id == "" {
		return camperrors.New("peer id must not be empty")
	}
	if strings.HasPrefix(id, ".") ||
		strings.ContainsAny(id, "/\\\x00") ||
		strings.Contains(id, "..") {
		return camperrors.Newf("invalid peer id %q", id)
	}
	return nil
}

// LoadSnapshot reads the last-transfer manifest for (peer, root). A missing
// snapshot returns (nil, nil): first sync with that peer.
func LoadSnapshot(campaignRoot, peerID, rootRel string) (*Manifest, error) {
	data, err := os.ReadFile(snapshotPath(campaignRoot, peerID, rootRel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, camperrors.Wrap(err, "read peer snapshot")
	}
	return DecodeManifest(data)
}

// ListSnapshotPeers returns the peer ids that have recorded snapshots in
// this campaign (the directories under .campaign/cache/peersync).
func ListSnapshotPeers(campaignRoot string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(campaignRoot, filepath.FromSlash(snapshotDir)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, camperrors.Wrap(err, "list snapshot peers")
	}
	var peers []string
	for _, e := range entries {
		if e.IsDir() {
			peers = append(peers, e.Name())
		}
	}
	return peers, nil
}

// SaveSnapshot records the manifest as the last-transfer state for (peer, root).
func SaveSnapshot(campaignRoot, peerID, rootRel string, m *Manifest) error {
	path := snapshotPath(campaignRoot, peerID, rootRel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return camperrors.Wrap(err, "create snapshot directory")
	}
	data, err := m.EncodeJSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "write peer snapshot")
	}
	return nil
}
