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
	slug := strings.ReplaceAll(NormalizeRootPath(rootRel), "/", "__")
	return filepath.Join(campaignRoot, filepath.FromSlash(snapshotDir), peerID, slug+".json")
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
